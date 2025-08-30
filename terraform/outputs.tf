# VPC Outputs
output "vpc_id" {
  description = "ID of the VPC"
  value       = google_compute_network.vpc.id
}

output "vpc_self_link" {
  description = "Self link of the VPC"
  value       = google_compute_network.vpc.self_link
}

output "private_subnet_id" {
  description = "ID of the private subnet"
  value       = google_compute_subnetwork.private.id
}

output "public_subnet_id" {
  description = "ID of the public subnet"
  value       = google_compute_subnetwork.public.id
}

output "private_subnet_cidr" {
  description = "CIDR of the private subnet"
  value       = google_compute_subnetwork.private.ip_cidr_range
}

output "public_subnet_cidr" {
  description = "CIDR of the public subnet"
  value       = google_compute_subnetwork.public.ip_cidr_range
}

# GKE Outputs
output "cluster_id" {
  description = "GKE cluster ID"
  value       = google_container_cluster.primary.id
}

output "cluster_name" {
  description = "GKE cluster name"
  value       = google_container_cluster.primary.name
}

output "cluster_endpoint" {
  description = "Endpoint for GKE control plane"
  value       = "https://${google_container_cluster.primary.endpoint}"
}

output "cluster_ca_certificate" {
  description = "Base64 encoded certificate data required to communicate with the cluster"
  value       = google_container_cluster.primary.master_auth.0.cluster_ca_certificate
}

output "cluster_token" {
  description = "GKE cluster auth token"
  value       = data.google_client_config.default.access_token
  sensitive   = true
}

output "cluster_location" {
  description = "GKE cluster location"
  value       = google_container_cluster.primary.location
}

# Artifact Registry Outputs
output "artifact_registry_urls" {
  description = "URLs of the Artifact Registry repositories"
  value = {
    for k, v in google_artifact_registry_repository.repositories : k => "${v.location}-docker.pkg.dev/${var.gcp_project_id}/${v.repository_id}"
  }
}

# Redis Outputs (Memorystore)
output "redis_host" {
  description = "Redis instance host"
  value       = google_redis_instance.redis.host
}

output "redis_port" {
  description = "Redis instance port"
  value       = google_redis_instance.redis.port
}

output "redis_auth_string" {
  description = "Redis AUTH string"
  value       = google_redis_instance.redis.auth_string
  sensitive   = true
}

output "redis_connection_string" {
  description = "Redis connection string"
  value       = "${google_redis_instance.redis.host}:${google_redis_instance.redis.port}"
}

# Cloud Storage Outputs
output "storage_bucket_name" {
  description = "Name of the Cloud Storage bucket"
  value       = google_storage_bucket.app_data.name
}

output "storage_bucket_url" {
  description = "URL of the Cloud Storage bucket"
  value       = google_storage_bucket.app_data.url
}

output "storage_bucket_self_link" {
  description = "Self link of the Cloud Storage bucket"
  value       = google_storage_bucket.app_data.self_link
}

# Service Account Outputs
output "pod_service_account_email" {
  description = "Email of the service account for pods"
  value       = google_service_account.pod_service_account.email
}

output "gke_nodes_service_account_email" {
  description = "Email of the service account for GKE nodes"
  value       = google_service_account.gke_nodes.email
}

# KMS Outputs
output "kms_key_ring_id" {
  description = "ID of the KMS key ring"
  value       = google_kms_key_ring.gke.id
}

output "gke_kms_key_id" {
  description = "ID of the GKE KMS key"
  value       = google_kms_crypto_key.gke.id
}

output "storage_kms_key_id" {
  description = "ID of the Storage KMS key"
  value       = google_kms_crypto_key.storage.id
}

# Network Outputs
output "nat_router_name" {
  description = "Name of the Cloud Router"
  value       = google_compute_router.router.name
}

output "nat_gateway_name" {
  description = "Name of the Cloud NAT gateway"
  value       = google_compute_router_nat.nat.name
}

# Kubeconfig
output "kubeconfig" {
  description = "kubectl config for GKE cluster"
  value = templatefile("${path.module}/kubeconfig.tpl", {
    cluster_name           = google_container_cluster.primary.name
    endpoint              = google_container_cluster.primary.endpoint
    certificate_authority = google_container_cluster.primary.master_auth.0.cluster_ca_certificate
    region                = var.gcp_region
    project_id            = var.gcp_project_id
  })
  sensitive = true
}