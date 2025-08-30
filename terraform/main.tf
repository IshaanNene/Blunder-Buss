terraform {
  required_version = ">= 1.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.0"
    }
  }
}

provider "google" {
  project = var.gcp_project_id
  region  = var.gcp_region
  zone    = var.gcp_zone
  
  default_labels = {
    project     = "blunder-buss-platform"
    environment = var.environment
    managed-by  = "terraform"
  }
}

# Data sources
data "google_client_config" "default" {}

# VPC Network
resource "google_compute_network" "vpc" {
  name                    = "${var.project_name}-vpc"
  auto_create_subnetworks = false
  routing_mode           = "REGIONAL"
}

# Private subnet
resource "google_compute_subnetwork" "private" {
  name          = "${var.project_name}-private-subnet"
  ip_cidr_range = var.private_subnet_cidr
  region        = var.gcp_region
  network       = google_compute_network.vpc.id
  
  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = var.pod_subnet_cidr
  }
  
  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = var.service_subnet_cidr
  }
  
  private_ip_google_access = true
}

# Public subnet
resource "google_compute_subnetwork" "public" {
  name          = "${var.project_name}-public-subnet"
  ip_cidr_range = var.public_subnet_cidr
  region        = var.gcp_region
  network       = google_compute_network.vpc.id
}

# Cloud Router for NAT Gateway
resource "google_compute_router" "router" {
  name    = "${var.project_name}-router"
  region  = var.gcp_region
  network = google_compute_network.vpc.id
}

# NAT Gateway
resource "google_compute_router_nat" "nat" {
  name                               = "${var.project_name}-nat"
  router                            = google_compute_router.router.name
  region                            = var.gcp_region
  nat_ip_allocate_option            = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"
  
  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}

# Firewall rules
resource "google_compute_firewall" "allow_internal" {
  name    = "${var.project_name}-allow-internal"
  network = google_compute_network.vpc.name
  
  allow {
    protocol = "tcp"
    ports    = ["0-65535"]
  }
  
  allow {
    protocol = "udp"
    ports    = ["0-65535"]
  }
  
  allow {
    protocol = "icmp"
  }
  
  source_ranges = [var.private_subnet_cidr, var.pod_subnet_cidr, var.service_subnet_cidr]
}

resource "google_compute_firewall" "allow_ssh" {
  name    = "${var.project_name}-allow-ssh"
  network = google_compute_network.vpc.name
  
  allow {
    protocol = "tcp"
    ports    = ["22"]
  }
  
  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["ssh-enabled"]
}

# Container Registry (Artifact Registry)
resource "google_artifact_registry_repository" "repositories" {
  for_each = toset(["api", "worker", "web", "stockfish"])
  
  location      = var.gcp_region
  repository_id = "${var.project_name}-${each.key}"
  description   = "Docker repository for ${each.key}"
  format        = "DOCKER"
  
  cleanup_policies {
    id     = "keep-last-30"
    action = "KEEP"
    most_recent_versions {
      keep_count = 30
    }
  }
  
  cleanup_policies {
    id     = "delete-untagged"
    action = "DELETE"
    condition {
      tag_state  = "UNTAGGED"
      older_than = "86400s" # 1 day
    }
  }
}

# GKE Cluster
resource "google_container_cluster" "primary" {
  name     = var.cluster_name
  location = var.gcp_region
  
  # We can't create a cluster with no node pool defined, but we want to only use
  # separately managed node pools. So we create the smallest possible default
  # node pool and immediately delete it.
  remove_default_node_pool = true
  initial_node_count       = 1
  
  network    = google_compute_network.vpc.name
  subnetwork = google_compute_subnetwork.private.name
  
  # IP allocation policy for VPC-native cluster
  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }
  
  # Network policy
  network_policy {
    enabled = true
  }
  
  # Enable Workload Identity
  workload_identity_config {
    workload_pool = "${var.gcp_project_id}.svc.id.goog"
  }
  
  # Enable network policy
  addons_config {
    network_policy_config {
      disabled = false
    }
  }
  
  # Master authorized networks
  master_authorized_networks_config {
    cidr_blocks {
      cidr_block   = "0.0.0.0/0"
      display_name = "All networks"
    }
  }
  
  # Enable private cluster
  private_cluster_config {
    enable_private_nodes    = true
    enable_private_endpoint = false
    master_ipv4_cidr_block  = "172.16.0.0/28"
  }
  
  # Logging and monitoring
  logging_service    = "logging.googleapis.com/kubernetes"
  monitoring_service = "monitoring.googleapis.com/kubernetes"
  
  # Security
  database_encryption {
    state    = "ENCRYPTED"
    key_name = google_kms_crypto_key.gke.id
  }
  
  # Maintenance window
  maintenance_policy {
    recurring_window {
      start_time = "2023-01-01T09:00:00Z"
      end_time   = "2023-01-01T17:00:00Z"
      recurrence = "FREQ=WEEKLY;BYDAY=SA,SU"
    }
  }
}

# GKE Node Pool
resource "google_container_node_pool" "primary_nodes" {
  name       = "${var.cluster_name}-node-pool"
  location   = var.gcp_region
  cluster    = google_container_cluster.primary.name
  node_count = var.node_group_desired_size
  
  autoscaling {
    min_node_count = var.node_group_min_size
    max_node_count = var.node_group_max_size
  }
  
  management {
    auto_repair  = true
    auto_upgrade = true
  }
  
  upgrade_settings {
    max_surge       = 1
    max_unavailable = 0
  }
  
  node_config {
    preemptible  = false
    machine_type = var.node_instance_type
    disk_size_gb = 50
    disk_type    = "pd-ssd"
    image_type   = "COS_CONTAINERD"
    
    # Google recommends custom service accounts that have cloud-platform scope and permissions granted via IAM Roles.
    service_account = google_service_account.gke_nodes.email
    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform"
    ]
    
    labels = {
      environment = var.environment
      node-group  = "main"
    }
    
    tags = ["ssh-enabled"]
    
    # Workload Identity
    workload_metadata_config {
      mode = "GKE_METADATA"
    }
    
    metadata = {
      disable-legacy-endpoints = "true"
    }
  }
}

# KMS Key for GKE encryption
resource "google_kms_key_ring" "gke" {
  name     = "${var.cluster_name}-keyring"
  location = var.gcp_region
}

resource "google_kms_crypto_key" "gke" {
  name     = "${var.cluster_name}-key"
  key_ring = google_kms_key_ring.gke.id
  
  lifecycle {
    prevent_destroy = true
  }
}

# Service Account for GKE nodes
resource "google_service_account" "gke_nodes" {
  account_id   = "${var.project_name}-gke-nodes"
  display_name = "GKE Node Service Account"
}

resource "google_project_iam_member" "gke_nodes" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
    "roles/monitoring.viewer",
    "roles/stackdriver.resourceMetadata.writer"
  ])
  
  project = var.gcp_project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.gke_nodes.email}"
}

# Memorystore Redis Instance
resource "google_redis_instance" "redis" {
  name           = "${var.project_name}-redis"
  tier           = var.redis_tier
  memory_size_gb = var.redis_memory_size_gb
  region         = var.gcp_region
  
  authorized_network = google_compute_network.vpc.id
  connect_mode       = "PRIVATE_SERVICE_ACCESS"
  
  redis_version     = "REDIS_7_0"
  display_name      = "Chess AI Platform Redis"
  
  maintenance_policy {
    weekly_maintenance_window {
      day = "SUNDAY"
      start_time {
        hours   = 5
        minutes = 0
        seconds = 0
        nanos   = 0
      }
    }
  }
  
  persistence_config {
    persistence_mode = "RDB"
    rdb_snapshot_period = "TWENTY_FOUR_HOURS"
    rdb_snapshot_start_time = "03:00"
  }
  
  labels = {
    environment = var.environment
    project     = var.project_name
  }
}

# Private Service Connection for Redis
resource "google_compute_global_address" "private_ip_address" {
  name          = "${var.project_name}-private-ip"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.vpc.id
}

resource "google_service_networking_connection" "private_vpc_connection" {
  network                 = google_compute_network.vpc.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_ip_address.name]
}

# Cloud Storage Bucket
resource "google_storage_bucket" "app_data" {
  name          = "${var.project_name}-app-data-${random_id.bucket_suffix.hex}"
  location      = var.gcp_region
  force_destroy = false
  
  versioning {
    enabled = true
  }
  
  encryption {
    default_kms_key_name = google_kms_crypto_key.storage.id
  }
  
  lifecycle_rule {
    condition {
      age = 30
    }
    action {
      type = "Delete"
    }
  }
  
  lifecycle_rule {
    condition {
      num_newer_versions = 3
    }
    action {
      type = "Delete"
    }
  }
  
  labels = {
    environment = var.environment
    project     = var.project_name
  }
}

# Block public access to bucket
resource "google_storage_bucket_iam_binding" "prevent_public_read" {
  bucket = google_storage_bucket.app_data.name
  role   = "roles/storage.legacyBucketReader"
  
  members = []
}

# KMS key for Cloud Storage
resource "google_kms_crypto_key" "storage" {
  name     = "${var.project_name}-storage-key"
  key_ring = google_kms_key_ring.gke.id
  
  lifecycle {
    prevent_destroy = true
  }
}

# Random ID for bucket suffix
resource "random_id" "bucket_suffix" {
  byte_length = 4
}

# Service Account for application pods
resource "google_service_account" "pod_service_account" {
  account_id   = "${var.project_name}-pod-sa"
  display_name = "Service Account for application pods"
}

# IAM policy for pod service account
resource "google_project_iam_member" "pod_storage_access" {
  project = var.gcp_project_id
  role    = "roles/storage.objectAdmin"
  member  = "serviceAccount:${google_service_account.pod_service_account.email}"
  
  condition {
    title       = "Storage bucket access"
    description = "Access to specific bucket only"
    expression  = "resource.name.startsWith(\"projects/_/buckets/${google_storage_bucket.app_data.name}\")"
  }
}

resource "google_project_iam_member" "pod_artifact_registry_access" {
  project = var.gcp_project_id
  role    = "roles/artifactregistry.reader"
  member  = "serviceAccount:${google_service_account.pod_service_account.email}"
}

# Workload Identity binding
resource "google_service_account_iam_binding" "workload_identity" {
  service_account_id = google_service_account.pod_service_account.name
  role               = "roles/iam.workloadIdentityUser"
  
  members = [
    "serviceAccount:${var.gcp_project_id}.svc.id.goog[default/pod-service-account]",
  ]
}

# Enable required APIs
resource "google_project_service" "apis" {
  for_each = toset([
    "container.googleapis.com",
    "compute.googleapis.com",
    "redis.googleapis.com",
    "storage.googleapis.com",
    "artifactregistry.googleapis.com",
    "cloudkms.googleapis.com",
    "servicenetworking.googleapis.com"
  ])
  
  project = var.gcp_project_id
  service = each.value
  
  disable_dependent_services = true
}

# Firewall rule for Redis access
resource "google_compute_firewall" "redis_access" {
  name    = "${var.project_name}-redis-access"
  network = google_compute_network.vpc.name
  
  allow {
    protocol = "tcp"
    ports    = ["6379"]
  }
  
  source_ranges = [var.private_subnet_cidr, var.pod_subnet_cidr]
  target_tags   = ["redis-client"]
}