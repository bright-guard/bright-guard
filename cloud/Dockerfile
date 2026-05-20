# syntax=docker/dockerfile:1.7

# ── Stage 1: build the React SPA ─────────────────────────────────────────
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# Production build base path is the root of the served domain. Note: anything
# placed in cloud/web/public/ (e.g. robots.txt) is copied verbatim into dist/
# by Vite and ends up in the embedded SPA below.
RUN npm run build

# ── Stage 2: build the Go binary, embedding the SPA bundle ───────────────
FROM golang:1.25-alpine AS api
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api/ ./
# Drop the built SPA into the embed location used by internal/spa.
RUN rm -rf ./internal/spa/dist
COPY --from=web /web/dist ./internal/spa/dist
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# ── Stage 3: runtime ─────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=api /out/api /api
ENV PORT=8080 SERVE_SPA=true
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]
