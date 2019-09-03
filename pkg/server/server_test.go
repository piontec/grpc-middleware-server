package server_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"testing"
	"time"

	grpc_health_v1 "github.com/piontec/grpc-middleware-server/pkg/healthcheck/proto"
	"github.com/piontec/grpc-middleware-server/pkg/server"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

type testHelper struct {
	options    *server.GrpcServerOptions
	client     grpc_health_v1.HealthClient
	clientConn *grpc.ClientConn
	server     *server.GrpcServer
}

func getTestHelper(options *server.GrpcServerOptions) *testHelper {
	grpcAddr := fmt.Sprintf("localhost:%d", options.GrpcPort)

	server := server.NewGrpcServer(func(*grpc.Server) {}, options)
	go func() {
		server.Run()
	}()
	for {
		if server.IsStarted() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	requestOpts := grpc.WithInsecure()
	conn, err := grpc.Dial(grpcAddr, requestOpts)
	if err != nil {
		log.Fatalf("Unable to establish client connection to %s: %v", grpcAddr, err)
	}
	client := grpc_health_v1.NewHealthClient(conn)

	return &testHelper{
		options:    options,
		client:     client,
		clientConn: conn,
		server:     server,
	}
}

func (th *testHelper) cleanup() {
	th.clientConn.Close()
	th.server.Stop()
}

func TestHealthcheck(t *testing.T) {
	h := getTestHelper(&server.GrpcServerOptions{
		GrpcPort: 8090,
	})
	defer h.cleanup()

	resp, err := h.client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	assert.Nil(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.GetStatus())
}

func TestMetrics(t *testing.T) {
	h := getTestHelper(&server.GrpcServerOptions{
		GrpcPort:    8090,
		MetricsPort: 9090,
	})
	defer h.cleanup()

	_, err := h.client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	assert.Nil(t, err)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", 9090))
	assert.Nil(t, err)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Contains(t, string(body), "grpc_server_handled_total{grpc_code=\"OK\",grpc_method=\"Check\",grpc_service=\"grpc.health.v1.Health\",grpc_type=\"unary\"}")
}
