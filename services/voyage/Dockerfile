# syntax=docker/dockerfile:1

# Build the static voyage binary and fetch grpc_health_probe for the compose
# healthcheck. Context is the repo root (single Go module), so this Dockerfile
# can see go.mod and every package it imports.
FROM golang:1.26 AS build
WORKDIR /src

COPY . .
ENV CGO_ENABLED=0 GOOS=linux
# go build fetches only the voyage service's deps (not the go.mod tool graph).
RUN go build -trimpath -o /out/voyage ./services/voyage
RUN GOBIN=/out go install github.com/grpc-ecosystem/grpc-health-probe@v0.4.52

# Distroless static, non-root: just the binary and the health probe.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/voyage /voyage
COPY --from=build /out/grpc-health-probe /bin/grpc_health_probe
EXPOSE 9000
ENTRYPOINT ["/voyage"]
