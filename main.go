package main

import (
	"fmt"
	"net"
	"os"

	"github.com/gdong42/grpc-mate/http"
	"github.com/gdong42/grpc-mate/proxy"
	"go.uber.org/zap"

	"github.com/gdong42/grpc-mate/log"
	"github.com/kelseyhightower/envconfig"
	"google.golang.org/grpc"
)

// EnvConfig has all Environment variables that grpc-mate reads
type EnvConfig struct {
	// Port the HTTP Port grpc-mate listens on, defaults to 6666
	Port int `envconfig:"PORT" default:"6666"`
	// GrpcServerHost the backend gRPC Host grpc-mate connects to, defaults to 127.0.0.1
	GrpcServerHost string `envconfig:"GRPC_SERVER_HOST" default:"127.0.0.1"`
	// GrpcServerPort the backend gRPC Port grpc-mate connects to
	GrpcServerPort int `envconfig:"GRPC_SERVER_PORT" default:"9090"`
	// LogLevel the log level, must be INFO, DEBUG, or ERROR, defaults to INFO
	LogLevel string `envconfig:"LOG_LEVEL" default:"INFO"`
}

func main() {

	var env EnvConfig
	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Failed to read environment variables: %s\n", err.Error())
		os.Exit(1)
	}

	logger, err := log.NewLogger(env.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to create logger: %s\n", err)
		os.Exit(1)
	}

	grpcAddr := fmt.Sprintf("%s:%d", env.GrpcServerHost, env.GrpcServerPort)
	logger.Info("Connecting to gRPC service...", zap.String("grpc_addr", grpcAddr))

	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
	if err != nil {
		logger.Fatal("Could not connect to gRPC service", zap.String("grpc_addr", grpcAddr))
	}
	defer conn.Close()

	proxy := proxy.NewProxy(conn)

	s := http.New(proxy, logger)
	logger.Info("starting grpc-mate",
		zap.String("log_level", env.LogLevel),
		zap.Int("port", env.Port),
	)

	logger.Info("gRPC Mate Serving on %d...", zap.Int("port", env.Port))
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", env.Port))
	if err != nil {
		logger.Fatal("[FATAL] Failed to listen HTTP port \n", zap.Int("port", env.Port), zap.Error(err))
		os.Exit(1)
	}
	s.Serve(ln)
}
