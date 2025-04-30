#!/usr/bin/env bash
set -euo pipefail

IMAGE="dn010590sas/booking-queue-manager:latest"
RELEASE_NAME="booking-queue-manager"
NAMESPACE="default"
CHART_PATH="./helm/booking-queue-manager"

echo "➡️ Building Docker image: $IMAGE"
docker build -t "$IMAGE" .

echo "➡️ Pushing Docker image to registry"
docker push "$IMAGE"

echo "➡️ Deploying Helm chart: $RELEASE_NAME"
helm upgrade --install \
  --namespace "$NAMESPACE" \
  --create-namespace \
  "$RELEASE_NAME" \
  "$CHART_PATH" \
  --set image.repository="dn010590sas/bookingprocessor" \
  --set image.tag="latest"

echo "✅ Deployment complete!"
