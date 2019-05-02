package http

import (
	"context"
	"net"
	"net/http"

	"github.com/gdong42/grpc-mate/metadata"
	"go.uber.org/zap"
)

// GrpcClient is a dynamic gRPC client that performs reflection
type GrpcClient interface {
	IsReady() bool
	Invoke(context.Context,
		string,
		string,
		[]byte,
		*metadata.Metadata,
	) ([]byte, error)
}

// Server is a grpc-mate server
type Server struct {
	router     *http.ServeMux
	grpcClient GrpcClient
	logger     *zap.Logger
}

// New creates a new grpc-mate server
func New(grpcClient GrpcClient, logger *zap.Logger) *Server {
	s := &Server{
		router: http.NewServeMux(),
		logger: logger,
	}
	s.registerHandlers(grpcClient)
	return s
}

// Serve starts the Server and serves requests
func (s *Server) Serve(ln net.Listener) error {
	srv := &http.Server{
		Handler: s.router,
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
