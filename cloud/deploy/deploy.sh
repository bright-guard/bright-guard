#!/usr/bin/env bash
# Build the image with Cloud Build, deploy to Cloud Run.
# Run from the cloud/ directory:  ./deploy/deploy.sh
set -euo pipefail

PROJECT=${PROJECT:-bright-guard-prod}
REGION=${REGION:-us-central1}
SERVICE=${SERVICE:-bright-guard}
REPO=${REPO:-bright-guard}
SQL_INSTANCE=${SQL_INSTANCE:-bright-guard-db}
IMAGE="${REGION}-docker.pkg.dev/${PROJECT}/${REPO}/${SERVICE}:$(date -u +%Y%m%d-%H%M%S)"
SQL_CONNECTION="${PROJECT}:${REGION}:${SQL_INSTANCE}"

echo "==> building ${IMAGE}"
gcloud builds submit --project="${PROJECT}" --tag="${IMAGE}" .

# Resolve service URL ahead of time so APP_BASE_URL is set in the first deploy.
EXISTING_URL=$(gcloud run services describe "${SERVICE}" \
  --project="${PROJECT}" --region="${REGION}" \
  --format='value(status.url)' 2>/dev/null || true)

# Honor a custom domain if one is already configured via env vars on the
# existing revision. This lets `BASE_URL=https://your.domain` override, and
# also preserves whatever is currently set so we don't clobber it on redeploy.
EXISTING_APP_BASE=$(gcloud run services describe "${SERVICE}" \
  --project="${PROJECT}" --region="${REGION}" \
  --format='value(spec.template.spec.containers[0].env)' 2>/dev/null \
  | tr ';' '\n' | grep "'name': 'APP_BASE_URL'" \
  | sed "s/.*'value': '\([^']*\)'.*/\1/" | head -1)

BASE_URL=${BASE_URL:-${EXISTING_APP_BASE:-${EXISTING_URL:-https://${SERVICE}-${REGION}.run.app}}}

echo "==> deploying ${SERVICE} (base URL: ${BASE_URL})"

# Build database URL using the Cloud SQL unix socket connector.
DB_USER=brightguard
DB_NAME=brightguard
DATABASE_URL_TEMPLATE="postgres://${DB_USER}:__PASSWORD__@/${DB_NAME}?host=/cloudsql/${SQL_CONNECTION}&sslmode=disable"

DB_PASSWORD=$(gcloud secrets versions access latest --secret=db-password --project="${PROJECT}")
DATABASE_URL="${DATABASE_URL_TEMPLATE/__PASSWORD__/${DB_PASSWORD}}"

gcloud run deploy "${SERVICE}" \
  --project="${PROJECT}" \
  --region="${REGION}" \
  --image="${IMAGE}" \
  --platform=managed \
  --allow-unauthenticated \
  --port=8080 \
  --memory=512Mi \
  --cpu=1 \
  --min-instances=0 \
  --max-instances=4 \
  --add-cloudsql-instances="${SQL_CONNECTION}" \
  --set-env-vars="APP_BASE_URL=${BASE_URL},WEB_BASE_URL=${BASE_URL},SESSION_COOKIE_SECURE=true,DEV_LOGIN_ENABLED=false,SERVE_SPA=true" \
  --set-env-vars="^|^ALLOWED_HOSTS=${ALLOWED_HOSTS:-mcp-governance.infoblox.dev,bright-guard-cy6ozp2w3a-uc.a.run.app}" \
  --set-env-vars="^~^DATABASE_URL=${DATABASE_URL}" \
  --update-secrets="SESSION_SECRET=session-secret:latest,GOOGLE_CLIENT_ID=google-client-id:latest,GOOGLE_CLIENT_SECRET=google-client-secret:latest"

echo
echo "==> deployed: ${BASE_URL}"
echo "==> healthz:  ${BASE_URL}/api/healthz"
