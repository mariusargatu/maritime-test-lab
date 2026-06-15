// Package app wires the gateway: a gRPC client to the voyage service (the only
// edge, D-004), a gqlgen GraphQL server at /query, and a /healthz probe. Runs
// until the context is cancelled, then shuts the HTTP server down gracefully.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"maritime-test-lab/gateway/graph"
	"maritime-test-lab/gen/graphql"
	voyagev1 "maritime-test-lab/gen/proto/voyage/v1"
	"maritime-test-lab/internal/tracing"
)

// Run starts the gateway and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg Config) error {
	shutdownTracing, err := tracing.Init(ctx, "gateway", cfg.OTLPEndpoint)
	if err != nil {
		return fmt.Errorf("gateway app: %w", err)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	conn, err := grpc.NewClient(cfg.VoyageAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return fmt.Errorf("gateway app: dial voyage: %w", err)
	}
	defer func() { _ = conn.Close() }()

	resolver := &graph.Resolver{VoyageClient: voyagev1.NewVoyageServiceClient(conn)}

	srv := handler.New(graphql.NewExecutableSchema(graphql.Config{Resolvers: resolver}))
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.GET{})
	srv.Use(extension.Introspection{})

	mux := http.NewServeMux()
	// otelhttp starts the server-side trace at the GraphQL HTTP request.
	mux.Handle("/query", otelhttp.NewHandler(srv, "graphql"))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("gateway app: serve: %w", err)
	}
	return nil
}
