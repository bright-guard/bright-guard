#!/usr/bin/env bash
# Build and push the shim image to Artifact Registry as a multi-arch manifest.
# Run from the cloud/shim/ directory:  ./deploy.sh
set -euo pipefail

PROJECT=${PROJECT:-bright-guard-prod}
REGION=${REGION:-us-central1}
REPO=${REPO:-bright-guard}
NAME=${NAME:-bright-guard-shim}
PLATFORMS=${PLATFORMS:-linux/amd64,linux/arm64}
BUILDER=${BUILDER:-bg-multiarch}
TS=$(date -u +%Y%m%d-%H%M%S)
TAGGED="${REGION}-docker.pkg.dev/${PROJECT}/${REPO}/${NAME}:${TS}"
LATEST="${REGION}-docker.pkg.dev/${PROJECT}/${REPO}/${NAME}:latest"

# Ensure a multi-platform buildx builder exists.
if ! docker buildx inspect "${BUILDER}" >/dev/null 2>&1; then
  echo "==> creating buildx builder ${BUILDER}"
  docker buildx create --name "${BUILDER}" --driver docker-container --bootstrap
fi

echo "==> building ${TAGGED} for ${PLATFORMS}"
docker buildx build \
  --builder "${BUILDER}" \
  --platform "${PLATFORMS}" \
  --tag "${TAGGED}" \
  --tag "${LATEST}" \
  --push \
  .

echo
echo "==> pushed: ${TAGGED}"
echo "==> pushed: ${LATEST}"
