package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"github.com/gdong42/grpc-mate/http"
	"github.com/gdong42/grpc-mate/proxy"
	"go.uber.org/zap"

	"github.com/gdong42/grpc-mate/log"
	"github.com/kelseyhightower/envconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// EnvConfig has all Environment variables that grpc-mate reads
type EnvConfig struct {
	// Port the HTTP Port grpc-mate listens on, defaults to 6600
	Port int `envconfig:"GRPC_MATE_PORT" default:"6600"`
	// GrpcServerHost the backend gRPC Host grpc-mate connects to, defaults to 127.0.0.1
	GrpcServerHost string `envconfig:"GRPC_MATE_PROXIED_HOST" default:"127.0.0.1"`
	// GrpcServerPort the backend gRPC Port grpc-mate connects to, defaults to 9090
	GrpcServerPort int `envconfig:"GRPC_MATE_PROXIED_PORT" default:"9090"`
	// GrpcServerTLS enables verified TLS to the backend, defaults to false
	GrpcServerTLS bool `envconfig:"GRPC_MATE_PROXIED_TLS_ENABLED" default:"false"`
	// GrpcServerTLSCAFile is an optional PEM bundle appended to system roots
	GrpcServerTLSCAFile string `envconfig:"GRPC_MATE_PROXIED_TLS_CA_FILE"`
	// LogLevel the log level, must be INFO, DEBUG, or ERROR, defaults to INFO
	LogLevel string `envconfig:"GRPC_MATE_LOG_LEVEL" default:"INFO"`
}

func main() {

	var env EnvConfig
	if err := envconfig.Process("", &env); err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Failed to read environment variables: %s\n", err.Error())
		os.Exit(1)
	}

	roots, err := upstreamRoots(env.GrpcServerTLS, env.GrpcServerTLSCAFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Invalid upstream TLS configuration: %s\n", err)
		os.Exit(1)
	}

	logger, err := log.NewLogger(env.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to create logger: %s\n", err)
		os.Exit(1)
	}

	grpcAddr := fmt.Sprintf("%s:%d", env.GrpcServerHost, env.GrpcServerPort)
	logger.Info("Connecting to gRPC service...", zap.String("grpc_addr", grpcAddr))

	conn, err := grpc.Dial(grpcAddr, upstreamTransport(env.GrpcServerTLS, roots))
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

// upstreamTransport preserves plaintext by default. With TLS enabled, nil RootCAs
// uses system roots and gRPC derives the verified server name from the target.
func upstreamTransport(useTLS bool, roots *x509.CertPool) grpc.DialOption {
	if useTLS {
		return grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: roots}))
	}
	return grpc.WithInsecure()
}

// upstreamRoots leaves default system trust unchanged unless a CA file is set.
func upstreamRoots(useTLS bool, caFile string) (*x509.CertPool, error) {
	if caFile == "" {
		return nil, nil
	}
	if !useTLS {
		return nil, fmt.Errorf("GRPC_MATE_PROXIED_TLS_CA_FILE requires GRPC_MATE_PROXIED_TLS_ENABLED=true")
	}
	data, err := ioutil.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read upstream CA file %q: %v", caFile, err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system CA certificates: %v", err)
	}
	if !roots.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("upstream CA file %q contains no valid PEM certificates", caFile)
	}
	return roots, nil
}
