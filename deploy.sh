#!/bin/bash
set -e

AWS_REGION="us-east-1"
AWS_ACCOUNT_ID="<YOUR_AWS_ACCOUNT_ID>"
ECR_REPO_PREFIX="Blunder-Buss"
NAMESPACE="stockfish"

export API_IMAGE="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/$ECR_REPO_PREFIX-api:latest"
export WORKER_IMAGE="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/$ECR_REPO_PREFIX-worker:latest"
export STOCKFISH_IMAGE="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/$ECR_REPO_PREFIX-stockfish:latest"
export WEB_IMAGE="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/$ECR_REPO_PREFIX-web:latest"

for file in ./k8s/*-deployment.yaml; do
    envsubst < $file | kubectl apply -f -
done


SERVICES=("api" "worker" "stockfish" "web")

echo "Building Docker images..."
for svc in "${SERVICES[@]}"; do
    echo "Building $svc..."
    docker build -t $svc:latest ./docker/$svc
done

echo "Logging into AWS ECR..."
aws ecr get-login-password --region $AWS_REGION | docker login --username AWS --password-stdin $AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com

for svc in "${SERVICES[@]}"; do
    aws ecr describe-repositories --repository-names $ECR_REPO_PREFIX-$svc --region $AWS_REGION >/dev/null 2>&1 || \
        aws ecr create-repository --repository-name $ECR_REPO_PREFIX-$svc --region $AWS_REGION
done

echo "Tagging and pushing Docker images..."
for svc in "${SERVICES[@]}"; do
    ECR_IMAGE="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/$ECR_REPO_PREFIX-$svc:latest"
    docker tag $svc:latest $ECR_IMAGE
    docker push $ECR_IMAGE
done

echo "Updating Kubernetes manifests with ECR images..."
for svc in "${SERVICES[@]}"; do
    ECR_IMAGE="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/$ECR_REPO_PREFIX-$svc:latest"
    find ./k8s -type f -name "*-deployment.yaml" -exec sed -i.bak "s|image: $svc:latest|image: $ECR_IMAGE|g" {} +
done

echo "Applying Kubernetes manifests..."
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/redis-deployment.yaml
kubectl apply -f k8s/stockfish-deployment.yaml
kubectl apply -f k8s/api-deployment.yaml
kubectl apply -f k8s/worker-deployment.yaml
kubectl apply -f k8s/web-deployment.yaml

kubectl apply -f k8s/hpa-stockfish.yaml || true
kubectl apply -f k8s/keda-scaledobject-queue.yaml || true

echo "✅ Deployment complete!"