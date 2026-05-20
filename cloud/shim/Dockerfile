# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/shim ./cmd/shim
# Create the runtime state dir owned by the distroless `nonroot` user (uid 65532).
RUN install -d -m 0700 -o 65532 -g 65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/shim /shim
COPY --from=build /out/data /data
COPY --from=build /src/examples/fake-servers.yaml /etc/brightguard/shim.yaml
VOLUME ["/data"]
ENV BG_STATE_DIR=/data
USER nonroot:nonroot
ENTRYPOINT ["/shim"]
