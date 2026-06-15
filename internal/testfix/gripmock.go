package testfix

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"maritime-test-lab/internal/repopath"
)

// StartGripmock runs bavix/gripmock loaded with the estimator proto. It returns
// the gRPC address (the stubbed estimator the voyage service dials) and the stub
// API URL (where suites purge and post stubs), plus a stop func.
func StartGripmock(ctx context.Context) (grpcAddr, stubsURL string, stop func(), err error) {
	protoPath, err := repopath.Find("proto/estimator/v1/estimator.proto")
	if err != nil {
		return "", "", nil, fmt.Errorf("testfix gripmock: %w", err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "bavix/gripmock:3.13.1",
		ExposedPorts: []string{"4770/tcp", "4771/tcp"},
		Cmd:          []string{"-S", "/proto/estimator.proto"},
		Files: []testcontainers.ContainerFile{{
			HostFilePath:      protoPath,
			ContainerFilePath: "/proto/estimator.proto",
			FileMode:          0o644,
		}},
		WaitingFor: wait.ForHTTP("/api/stubs").WithPort("4771/tcp").WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		return "", "", nil, fmt.Errorf("testfix gripmock: %w", err)
	}
	stop = func() { _ = c.Terminate(context.Background()) }

	host, err := c.Host(ctx)
	if err != nil {
		stop()
		return "", "", nil, fmt.Errorf("testfix gripmock host: %w", err)
	}
	grpcPort, err := c.MappedPort(ctx, "4770")
	if err != nil {
		stop()
		return "", "", nil, fmt.Errorf("testfix gripmock grpc port: %w", err)
	}
	apiPort, err := c.MappedPort(ctx, "4771")
	if err != nil {
		stop()
		return "", "", nil, fmt.Errorf("testfix gripmock api port: %w", err)
	}

	grpcAddr = net.JoinHostPort(host, grpcPort.Port())
	stubsURL = fmt.Sprintf("http://%s/api/stubs", net.JoinHostPort(host, apiPort.Port()))
	return grpcAddr, stubsURL, stop, nil
}
