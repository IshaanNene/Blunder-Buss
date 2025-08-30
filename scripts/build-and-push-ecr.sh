#!/bin/bash
set -e

REGION=${1:-us-east-1}
ACCOUNT_ID=${2:-$(aws sts get-caller-identity --query Account --output text)}
PROJECT_NAME="blunder-buss"

SERVICES=("api" "worker" "web" "stockfish")

echo "Building and pushing images to ECR..."
echo "Region: $REGION"
echo "Account ID: $ACCOUNT_ID"
echo "Project: $PROJECT_NAME"

echo "Logging in to ECR..."
aws ecr get-login-password --region $REGION | docker login --username AWS --password-stdin $ACCOUNT_ID.dkr.ecr.$REGION.amazonaws.com

for SERVICE in "${SERVICES[@]}"; do
    echo ""
    echo "Building $SERVICE..."
    docker build -f docker/$SERVICE/Dockerfile -t $SERVICE:latest .
    
    ECR_URI="$ACCOUNT_ID.dkr.ecr.$REGION.amazonaws.com/$PROJECT_NAME-$SERVICE"
    docker tag $SERVICE:latest $ECR_URI:latest
    docker tag $SERVICE:latest $ECR_URI:$(date +%Y%m%d-%H%M%S)
    
    echo "Pushing $SERVICE to ECR..."
    docker push $ECR_URI:latest
    docker push $ECR_URI:$(date +%Y%m%d-%H%M%S)
    
    echo "$SERVICE pushed successfully"
done

echo ""
echo "All images pushed successfully!"
echo ""
echo "Update your Kubernetes manifests with these images:"
for SERVICE in "${SERVICES[@]}"; do
    echo "  $SERVICE: $ACCOUNT_ID.dkr.ecr.$REGION.amazonaws.com/$PROJECT_NAME-$SERVICE:latest"
done