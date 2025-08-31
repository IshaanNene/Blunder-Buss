#!/bin/bash
set -e

REGION=${1:-us-central1}
PROJECT_ID=${2:-$(gcloud config get-value project)}
PROJECT_NAME="blunder-buss"

SERVICES=("api" "worker" "web" "stockfish")

echo "Building and pushing images to Artifact Registry..."
echo "Region: $REGION"
echo "Project ID: $PROJECT_ID"
echo "Project: $PROJECT_NAME"

# Ensure Artifact Registry API is enabled
gcloud services enable artifactregistry.googleapis.com --project $PROJECT_ID

# Create repo if not exists
REPO_NAME="${PROJECT_NAME}-repo"
if ! gcloud artifacts repositories describe $REPO_NAME --location=$REGION --project=$PROJECT_ID >/dev/null 2>&1; then
  echo "Creating Artifact Registry repo: $REPO_NAME"
  gcloud artifacts repositories create $REPO_NAME \
    --repository-format=docker \
    --location=$REGION \
    --description="Docker images for $PROJECT_NAME"
fi

# Configure Docker to use gcloud auth for Artifact Registry
gcloud auth configure-docker $REGION-docker.pkg.dev --quiet

for SERVICE in "${SERVICES[@]}"; do
    echo ""
    echo "Building $SERVICE..."
    docker build -f docker/$SERVICE/Dockerfile -t $SERVICE:latest .
    
    AR_URI="$REGION-docker.pkg.dev/$PROJECT_ID/$REPO_NAME/$PROJECT_NAME-$SERVICE"
    docker tag $SERVICE:latest $AR_URI:latest
    docker tag $SERVICE:latest $AR_URI:$(date +%Y%m%d-%H%M%S)
    
    echo "Pushing $SERVICE to Artifact Registry..."
    docker push $AR_URI:latest
    docker push $AR_URI:$(date +%Y%m%d-%H%M%S)
    
    echo "$SERVICE pushed successfully"
done

echo ""
echo "All images pushed successfully!"
echo ""
echo "Update your Kubernetes manifests with these images:"
for SERVICE in "${SERVICES[@]}"; do
    echo "  $SERVICE: $REGION-docker.pkg.dev/$PROJECT_ID/$REPO_NAME/$PROJECT_NAME-$SERVICE:latest"
done