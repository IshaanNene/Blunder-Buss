package k8s

import (
	"context"
	"os"
	"sync"
	"time"

	"stockfish-scale/pkg/logging"
	"stockfish-scale/pkg/metrics"
	
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ReplicaTracker tracks replica counts and calculates averages
// Requirement 5.4: Query Kubernetes API for current replica counts and calculate average replicas over 1-hour windows
type ReplicaTracker struct {
	clientset       *kubernetes.Clientset
	namespace       string
	metricsCol      *metrics.MetricsCollector
	logger          logging.Logger
	stopChan        chan struct{}
	
	// Replica count history for calculating averages
	replicaHistory  map[string][]replicaSnapshot
	historyMu       sync.RWMutex
	
	// Last known replica counts for detecting scaling events (Requirement 5.8)
	lastReplicaCounts map[string]int32
	lastCountsMu      sync.RWMutex
	
	// Scaling event counters for ratio calculation (Requirement 5.8)
	scaleUpCounts   map[string]int64
	scaleDownCounts map[string]int64
	scalingCountsMu sync.RWMutex
}

type replicaSnapshot struct {
	timestamp time.Time
	count     int32
}

// NewReplicaTracker creates a new replica tracker
func NewReplicaTracker(metricsCol *metrics.MetricsCollector, logger logging.Logger) (*ReplicaTracker, error) {
	// Create in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		// If not running in cluster, return nil (graceful degradation)
		logger.WithField("error", err.Error()).Warn("Not running in Kubernetes cluster, replica tracking disabled")
		return nil, nil
	}
	
	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	
	// Get namespace from environment or default to "stockfish"
	namespace := os.Getenv("K8S_NAMESPACE")
	if namespace == "" {
		namespace = "stockfish"
	}
	
	rt := &ReplicaTracker{
		clientset:         clientset,
		namespace:         namespace,
		metricsCol:        metricsCol,
		logger:            logger,
		stopChan:          make(chan struct{}),
		replicaHistory:    make(map[string][]replicaSnapshot),
		lastReplicaCounts: make(map[string]int32),
		scaleUpCounts:     make(map[string]int64),
		scaleDownCounts:   make(map[string]int64),
	}
	
	return rt, nil
}

// Start begins tracking replica counts
func (rt *ReplicaTracker) Start() {
	if rt == nil {
		return
	}
	
	rt.logger.WithField("namespace", rt.namespace).Info("Starting replica tracker")
	
	// Track these deployments
	deployments := []string{"api", "worker", "stockfish"}
	
	// Start tracking goroutine
	go rt.trackReplicas(deployments)
}

// Stop stops the replica tracker
func (rt *ReplicaTracker) Stop() {
	if rt == nil {
		return
	}
	
	close(rt.stopChan)
	rt.logger.Info("Replica tracker stopped")
}

// trackReplicas periodically queries Kubernetes API for replica counts
func (rt *ReplicaTracker) trackReplicas(deployments []string) {
	// Update every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	// Initial update
	rt.updateReplicaCounts(deployments)
	
	for {
		select {
		case <-rt.stopChan:
			return
		case <-ticker.C:
			rt.updateReplicaCounts(deployments)
		}
	}
}

// updateReplicaCounts queries Kubernetes API and updates metrics
func (rt *ReplicaTracker) updateReplicaCounts(deployments []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	for _, deploymentName := range deployments {
		// Get deployment
		deployment, err := rt.clientset.AppsV1().Deployments(rt.namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			rt.logger.WithFields(map[string]interface{}{
				"deployment": deploymentName,
				"error":      err.Error(),
			}).Warn("Failed to get deployment replica count")
			continue
		}
		
		// Get current replica count
		replicas := int32(0)
		if deployment.Spec.Replicas != nil {
			replicas = *deployment.Spec.Replicas
		}
		
		// Update current replica count metric (Requirement 5.4)
		rt.metricsCol.SetReplicaCount(deploymentName, float64(replicas))
		
		// Detect and track scaling events (Requirement 5.8)
		rt.detectScalingEvent(deploymentName, replicas)
		
		// Add to history
		rt.addToHistory(deploymentName, replicas)
		
		// Calculate and update average replicas over 1-hour window (Requirement 5.4)
		avgReplicas := rt.calculateAverageReplicas(deploymentName, time.Hour)
		rt.metricsCol.SetAverageReplicas(deploymentName, avgReplicas)
		
		rt.logger.WithFields(map[string]interface{}{
			"deployment":     deploymentName,
			"replicas":       replicas,
			"avg_1h":         avgReplicas,
		}).Debug("Updated replica metrics")
	}
}

// addToHistory adds a replica count snapshot to history
func (rt *ReplicaTracker) addToHistory(deployment string, count int32) {
	rt.historyMu.Lock()
	defer rt.historyMu.Unlock()
	
	snapshot := replicaSnapshot{
		timestamp: time.Now(),
		count:     count,
	}
	
	// Add to history
	rt.replicaHistory[deployment] = append(rt.replicaHistory[deployment], snapshot)
	
	// Clean up old entries (keep 2 hours of history)
	cutoff := time.Now().Add(-2 * time.Hour)
	history := rt.replicaHistory[deployment]
	
	// Find first index to keep
	keepFrom := 0
	for i, snap := range history {
		if snap.timestamp.After(cutoff) {
			keepFrom = i
			break
		}
	}
	
	// Trim old entries
	if keepFrom > 0 {
		rt.replicaHistory[deployment] = history[keepFrom:]
	}
}

// calculateAverageReplicas calculates the average replica count over a time window
func (rt *ReplicaTracker) calculateAverageReplicas(deployment string, window time.Duration) float64 {
	rt.historyMu.RLock()
	defer rt.historyMu.RUnlock()
	
	history := rt.replicaHistory[deployment]
	if len(history) == 0 {
		return 0
	}
	
	cutoff := time.Now().Add(-window)
	
	// Calculate weighted average based on time
	var totalWeightedCount float64
	var totalDuration float64
	
	for i := 0; i < len(history); i++ {
		if history[i].timestamp.Before(cutoff) {
			continue
		}
		
		// Calculate duration this count was active
		var duration time.Duration
		if i < len(history)-1 {
			duration = history[i+1].timestamp.Sub(history[i].timestamp)
		} else {
			duration = time.Since(history[i].timestamp)
		}
		
		totalWeightedCount += float64(history[i].count) * duration.Seconds()
		totalDuration += duration.Seconds()
	}
	
	if totalDuration == 0 {
		// Return current count if no history
		if len(history) > 0 {
			return float64(history[len(history)-1].count)
		}
		return 0
	}
	
	return totalWeightedCount / totalDuration
}

// detectScalingEvent detects and tracks scaling events
// Requirement 5.8: Count scale-up and scale-down events and calculate ratio for tuning analysis
func (rt *ReplicaTracker) detectScalingEvent(deployment string, currentCount int32) {
	rt.lastCountsMu.Lock()
	lastCount, exists := rt.lastReplicaCounts[deployment]
	
	// Update last known count
	rt.lastReplicaCounts[deployment] = currentCount
	rt.lastCountsMu.Unlock()
	
	// Skip if this is the first observation
	if !exists {
		return
	}
	
	// Detect scaling event
	if currentCount > lastCount {
		// Scale-up event
		rt.metricsCol.IncrementScalingEvents(deployment, "up")
		
		// Update internal counters for ratio calculation
		rt.scalingCountsMu.Lock()
		rt.scaleUpCounts[deployment]++
		rt.scalingCountsMu.Unlock()
		
		rt.logger.WithFields(map[string]interface{}{
			"deployment": deployment,
			"from":       lastCount,
			"to":         currentCount,
			"direction":  "up",
		}).Info("Scaling event detected")
		
		// Update ratio metric
		rt.updateScalingRatio(deployment)
		
	} else if currentCount < lastCount {
		// Scale-down event
		rt.metricsCol.IncrementScalingEvents(deployment, "down")
		
		// Update internal counters for ratio calculation
		rt.scalingCountsMu.Lock()
		rt.scaleDownCounts[deployment]++
		rt.scalingCountsMu.Unlock()
		
		rt.logger.WithFields(map[string]interface{}{
			"deployment": deployment,
			"from":       lastCount,
			"to":         currentCount,
			"direction":  "down",
		}).Info("Scaling event detected")
		
		// Update ratio metric
		rt.updateScalingRatio(deployment)
	}
}

// updateScalingRatio calculates and updates the scaling events ratio metric
// Requirement 5.8: Calculate ratio of scale-up events to scale-down events for tuning analysis
func (rt *ReplicaTracker) updateScalingRatio(deployment string) {
	rt.scalingCountsMu.RLock()
	scaleUpCount := rt.scaleUpCounts[deployment]
	scaleDownCount := rt.scaleDownCounts[deployment]
	rt.scalingCountsMu.RUnlock()
	
	// Calculate ratio
	var ratio float64
	if scaleDownCount == 0 {
		// If no scale-down events, ratio is infinite (represented as scale-up count)
		// This indicates the system is only scaling up, never down
		if scaleUpCount > 0 {
			ratio = float64(scaleUpCount)
		} else {
			ratio = 0
		}
	} else {
		// Normal case: ratio of scale-up to scale-down
		ratio = float64(scaleUpCount) / float64(scaleDownCount)
	}
	
	// Update metric
	rt.metricsCol.SetScalingEventsRatio(deployment, ratio)
	
	rt.logger.WithFields(map[string]interface{}{
		"deployment":       deployment,
		"scale_up_count":   scaleUpCount,
		"scale_down_count": scaleDownCount,
		"ratio":            ratio,
	}).Debug("Updated scaling events ratio")
}
