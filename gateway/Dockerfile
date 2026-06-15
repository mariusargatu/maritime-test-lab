# syntax=docker/dockerfile:1

# Build the static gateway binary. Context is the repo root (single Go module).
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -o /out/gateway ./gateway

# Alpine runtime: it ships busybox wget, which the compose /healthz check uses.
FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /out/gateway /gateway
USER app
EXPOSE 8080
ENTRYPOINT ["/gateway"]
