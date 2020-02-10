# gRPC middleware server

This project provides a ready to use, opinionated server stub that can be used to serve [go gRPC](https://github.com/grpc/grpc-go) services.

It is entirely based on the work done in [go-grpc-middleware](https://github.com/grpc-ecosystem/go-grpc-middleware).

The stack includes the following:

- absolutely no needed configuration (sane defaults), but configuration options are available if needed
- ability to register any gRPC service with user's custom function
- structured logging based on [logrus](https://github.com/sirupsen/logrus)
- prometheus monitoring enabled by default
- implementation of the [gRPC HealthCheck v1 protocol](https://github.com/grpc/grpc/blob/master/doc/health-checking.md)
- automatic panic recovery with option to provide custom recovery function
- authentication support with user provided custom function

## The same, but for HTTP

I created a similar starter setup for HTTP servers [over here](https://github.com/piontec/go-chi-middleware-server).

## Examples

Writing a simple server, that just offers the Health Check endpoint, uses default authentication and recovery functions and provides prometheus monitoring is as simple as:

```go
import "github.com/piontec/grpc-middleware-server/pkg/server"

func main() {
    server := server.NewGrpcServer(func(*grpc.Server) {}, nil)
    server.Run()
}
```

To register your own gRPC services, implement the registration function (which does nothing in the example above) like this:

```go
server := NewGrpcServer(func(server *grpc.Server) {
    clientMgr := &clnmgr.ClientManagerServer{}
    clnmgrpb.RegisterClientManagerServer(server, clientMgr)
}, nil)
```

Full code examples can be found in [server_test.go](./pkg/server/server_test.go).

## Configuration

The second argument to `NewGrpcServer()` is optional `GrpcServerOptions` struct. You can use it like that:

```go
server := NewGrpcServer(func(server *grpc.Server) {
    clientMgr := &clnmgr.ClientManagerServer{}
    clnmgrpb.RegisterClientManagerServer(server, clientMgr)
}, &server.GrpcServerOptions{
        GrpcPort:    8090, // change the default port for gRPC
        MetricsPort: 8091, // change the default port for prom metrics
        RecoveryHandler: func (panic interface{}) error {/* your code */ return nil}, // default one just logs the error
        AuthHandler: func (ctx context.Context) (context.Context, error) {/*non-empty error if auth failed*/ return ctx,nil},
        LoggerFields: map[string]interface{}{"ver": "v0.1.0"}, // map of additional fields added to each log message
        LogHealthCheckCalls: true, // by default false - calls to HealthCheck are not logged
        DisableHistogramMetrics: true, // by default false - disables per method execution time histograms, which are costly
        additionalOptions: []grpc.ServerOption, // any additional options to pass to grpc.NewServer(...) call
    }
)
```

## Health checking

It is easy to use the HealthCheck endpoint in a Docker image for Kubernetes or other healthchecks. Just install [grpc-health-probe](https://github.com/grpc-ecosystem/grpc-health-probe) and run:

```bash
grpc_health_probe -addr=localhost:8090
```
