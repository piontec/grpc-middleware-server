package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	grpc_log_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	grpc_prom "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	healthcheckpb "github.com/piontec/grpc-middleware-server/pkg/healthcheck/proto"
	healthcheck "github.com/piontec/grpc-middleware-server/pkg/healthcheck/server"
)

const (
	defaultGrpcPort    = 8090
	defaultMetricsPort = 8091
)

// GrpcServerOptions allows to override default GrpcServer options
type GrpcServerOptions struct {
	GrpcPort                int
	MetricsPort             int
	RecoveryHandler         grpc_recovery.RecoveryHandlerFunc
	AuthHandler             grpc_auth.AuthFunc
	LoggerFields            logrus.Fields
	LogHealthCheckCalls     bool
	DisableHistogramMetrics bool
	logger                  *logrus.Logger
	additionalOptions		[]grpc.ServerOption
}

func (o *GrpcServerOptions) fillDefaults(logger *logrus.Logger) {
	if o.GrpcPort == 0 {
		o.GrpcPort = defaultGrpcPort
	}
	if o.MetricsPort == 0 {
		o.MetricsPort = defaultMetricsPort
	}
	if o.RecoveryHandler == nil {
		o.RecoveryHandler = o.getDefaultRecoveryHandler(logger)
	}
	if o.AuthHandler == nil {
		o.AuthHandler = o.defaultAuthHandler
	}
}

func (o *GrpcServerOptions) defaultAuthHandler(ctx context.Context) (context.Context, error) {
	return ctx, nil
}

func (o *GrpcServerOptions) getDefaultRecoveryHandler(logger *logrus.Logger) func(panic interface{}) error {
	return func(panic interface{}) error {
		logger.Errorf("Recovering from panic: %v", panic)
		return nil
	}
}

// GrpcServer returns new opinionated gRPC server based on grpc-middleware
type GrpcServer struct {
	options            *GrpcServerOptions
	logger             *logrus.Logger
	server             *grpc.Server
	healthcheckService *healthcheck.SimpleHealthServer
	started            bool
	listener           net.Listener
	metricsServer      *http.Server
	stopChan           chan interface{}
}

// GetLogger returns a pointer to the logger used by the server
func (s *GrpcServer) GetLogger() *logrus.Logger {
	return s.logger
}

// NewGrpcServer returns a gRPC server optionally configured with GrpcServerOptions
func NewGrpcServer(servicesRegistrationHandler func(*grpc.Server), options *GrpcServerOptions) *GrpcServer {
	// initialize logrus as logger
	logger := logrus.New()
	logger.Formatter = &logrus.JSONFormatter{
		DisableTimestamp: true,
	}

	// if we didn't get any options, initialize with default struct
	if options == nil {
		options = &GrpcServerOptions{}
	}
	// initialize default options
	options.fillDefaults(logger)

	// if disabled, ignore logging to the healthcheck service
	if options.LogHealthCheckCalls {
		logger.Debug("Configuration: calls to the healthcheck endpoint won't be logged")
	}
	opts := []grpc_log_logrus.Option{
		grpc_log_logrus.WithDecider(func(methodFullName string, err error) bool {
			if !options.LogHealthCheckCalls && err == nil && methodFullName == "/grpc.health.v1.Health/Check" {
				return false
			}
			return true
		}),
	}

	recoveryOpts := []grpc_recovery.Option{
		grpc_recovery.WithRecoveryHandler(options.RecoveryHandler),
	}

	if !options.DisableHistogramMetrics {
		grpc_prom.EnableHandlingTimeHistogram()
	} else {
		logger.Debug("Configuration: prometheus histograms for method call duration are disabled")
	}

	logrusEntry := logrus.NewEntry(logger)
	if options.LoggerFields != nil {
		logrusEntry.WithFields(options.LoggerFields)
	}

	serverOptions := []grpc.ServerOption {
		grpc_middleware.WithUnaryServerChain(
			grpc_ctxtags.UnaryServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_log_logrus.UnaryServerInterceptor(logrusEntry, opts...),
			grpc_recovery.UnaryServerInterceptor(recoveryOpts...),
			grpc_auth.UnaryServerInterceptor(options.AuthHandler),
			grpc_prom.UnaryServerInterceptor,
		),
		grpc_middleware.WithStreamServerChain(
			grpc_ctxtags.StreamServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_log_logrus.StreamServerInterceptor(logrusEntry, opts...),
			grpc_recovery.StreamServerInterceptor(recoveryOpts...),
			grpc_auth.StreamServerInterceptor(options.AuthHandler),
			grpc_prom.StreamServerInterceptor,
		),
	}
	if len(options.additionalOptions) > 0 {
		serverOptions = append(serverOptions, options.additionalOptions...)
	}
	server := grpc.NewServer(serverOptions...)

	// register healthcheck gRPC service
	healthCheck := healthcheck.NewSimpleHealthServer(0)
	healthcheckpb.RegisterHealthServer(server, healthCheck)

	// run client's registration handler
	servicesRegistrationHandler(server)

	return &GrpcServer{
		options:            options,
		logger:             logger,
		server:             server,
		healthcheckService: healthCheck,
		stopChan:           make(chan interface{}, 1),
	}
}

// Run starts the listeners, blocks and waits for interruption signal to quit
func (s *GrpcServer) Run() {
	s.logger.Infof("Starting gRPC server on port :%d and metrics service on port :%d...",
		s.options.GrpcPort, s.options.MetricsPort)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.options.GrpcPort))
	s.listener = listener
	if err != nil {
		s.GetLogger().Fatalf("Unable to listen on gRPC port :%d: %v", s.options.GrpcPort, err)
	}
	go func() {
		if err := s.server.Serve(listener); err != nil {
			s.logger.Fatalf("Failed to serve: %v", err)
		}
	}()

	handler := http.NewServeMux()
	handler.Handle("/metrics", promhttp.Handler())
	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.options.MetricsPort),
		Handler: handler,
	}
	s.metricsServer = metricsServer
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil {
			if fmt.Sprint(err) != "http: Server closed" {
				s.logger.Fatalf("Failed to serve on metrics port: %v", err)
			}
		}
	}()

	s.healthcheckService.SetToServing()
	s.started = true
	s.logger.Infof("Server started")

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	select {
	case <-c:
	case <-s.stopChan:
	}

	s.Stop()
}

// Stop stops listening on server ports. Stopped server can't be Run() again.
func (s *GrpcServer) Stop() {
	if !s.started {
		return
	}
	s.logger.Infof("Stopping the server...")
	s.server.Stop()
	s.listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.metricsServer.Shutdown(ctx); err != nil {
		s.logger.Errorf("Error shutting down metrics port: %v", err)
	}
	s.started = false
	s.stopChan <- ""
	s.logger.Infof("Shutdown done")
}

// IsStarted returns true only of Run() was called and listeners are already started
func (s *GrpcServer) IsStarted() bool {
	return s.started
}
