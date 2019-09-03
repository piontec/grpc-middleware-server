// Package server provides a simple implementation of grpc healthchecking v1 protocol
package server

import (
	"context"
	"time"

	healthpb "github.com/piontec/grpc-middleware-server/pkg/healthcheck/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewSimpleHealthServer creates a new health server that returns NOT_SERVING.
// interval parameter is sued only for the Watch call to work. If you don't need Watch,
// pass 0 as interval and don't call Watch.
// Run SetToServing/SetToNotServing to switch the status.
func NewSimpleHealthServer(interval time.Duration) *SimpleHealthServer {
	var ticker *time.Ticker
	if int64(interval) > int64(0) {
		ticker = time.NewTicker(interval)
	}
	return &SimpleHealthServer{
		status: healthpb.HealthCheckResponse_NOT_SERVING,
		ticker: ticker,
	}
}

// SimpleHealthServer provides status info by keeping single status variable
type SimpleHealthServer struct {
	status healthpb.HealthCheckResponse_ServingStatus
	ticker *time.Ticker
}

// Check returns the current status kept by the server
func (s *SimpleHealthServer) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{
		Status: s.status,
	}, nil
}

// Watch returns a series of checks returning the current server status
func (s *SimpleHealthServer) Watch(_ *healthpb.HealthCheckRequest, srv healthpb.Health_WatchServer) error {
	if s.ticker == nil {
		return status.Errorf(codes.FailedPrecondition, "not configured to serve Watch")
	}
	for range s.ticker.C {
		srv.SendMsg(healthpb.HealthCheckResponse{
			Status: s.status,
		})
	}
	return nil
}

// SetToServing checks internal state kept and returned by the server to SERVING
func (s *SimpleHealthServer) SetToServing() {
	s.status = healthpb.HealthCheckResponse_SERVING
}

// SetToNotServing checks internal state kept and returned by the server to NOT_SERVING
func (s *SimpleHealthServer) SetToNotServing() {
	s.status = healthpb.HealthCheckResponse_NOT_SERVING
}
