// Package serve wires and runs a gRPC server with the standard health service
// and server reflection registered, shutting down gracefully when its context
// is cancelled. Both services share it so the health/reflection wiring — which
// Venom's grpc executor and grpc_health_probe both depend on — lives in one
// readable place.
package serve

import (
	"context"
	"fmt"
	"net"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// GRPC listens on addr, lets register attach service implementations, then
// serves until ctx is cancelled. Health is reported SERVING for the whole
// server and reflection is always on. On cancellation it stops gracefully.
func GRPC(ctx context.Context, addr string, register func(*grpc.Server)) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("serve grpc: listen %s: %w", addr, err)
	}

	// otelgrpc stats handler: incoming RPCs continue the caller's trace.
	server := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	register(server)

	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthSvc)
	reflection.Register(server)

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(lis) }()

	select {
	case <-ctx.Done():
		server.GracefulStop()
		return nil
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve grpc: %w", err)
		}
		return nil
	}
}
