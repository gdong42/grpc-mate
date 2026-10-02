package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	"github.com/gdong42/grpc-mate/metadata"
	"github.com/gdong42/grpc-mate/proxy"
	"github.com/golang/protobuf/proto"
	"github.com/kelseyhightower/envconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	pb "google.golang.org/grpc/test/grpc_testing"
)

type upstreamTestServer struct{ pb.TestServiceServer }

func (*upstreamTestServer) EmptyCall(context.Context, *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}

func TestTLSConfiguration(t *testing.T) {
	old, exists := os.LookupEnv("GRPC_MATE_PROXIED_TLS")
	defer func() {
		if exists {
			os.Setenv("GRPC_MATE_PROXIED_TLS", old)
		} else {
			os.Unsetenv("GRPC_MATE_PROXIED_TLS")
		}
	}()
	for _, tc := range []struct {
		value                string
		unset, want, invalid bool
	}{
		{unset: true}, {value: "false"}, {value: "true", want: true}, {value: "invalid", invalid: true},
	} {
		if tc.unset {
			os.Unsetenv("GRPC_MATE_PROXIED_TLS")
		} else {
			os.Setenv("GRPC_MATE_PROXIED_TLS", tc.value)
		}
		var env EnvConfig
		err := envconfig.Process("", &env)
		if (err != nil) != tc.invalid || env.GrpcServerTLS != tc.want {
			t.Fatalf("value %q: config=%+v, err=%v", tc.value, env, err)
		}
	}
}

func testCertificate(t *testing.T, hostname string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: root.NotBefore, NotAfter: root.NotAfter, DNSNames: []string{hostname}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	cert, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return tls.Certificate{Certificate: [][]byte{cert, der}, PrivateKey: key}, roots
}

func TestUpstreamTransport(t *testing.T) {
	cert, roots := testCertificate(t, "localhost")
	wrongCert, wrongRoots := testCertificate(t, "wrong.example")
	for _, tc := range []struct {
		name                 string
		serverTLS, clientTLS bool
		cert                 tls.Certificate
		roots                *x509.CertPool
		fail                 bool
	}{
		{name: "default plaintext"},
		{name: "trusted TLS", serverTLS: true, clientTLS: true, cert: cert, roots: roots},
		{name: "untrusted TLS", serverTLS: true, clientTLS: true, cert: cert, roots: x509.NewCertPool(), fail: true},
		{name: "hostname mismatch", serverTLS: true, clientTLS: true, cert: wrongCert, roots: wrongRoots, fail: true},
		{name: "TLS never falls back to plaintext", clientTLS: true, roots: roots, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var opts []grpc.ServerOption
			if tc.serverTLS {
				opts = append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{tc.cert}})))
			}
			server := grpc.NewServer(opts...)
			pb.RegisterTestServiceServer(server, &upstreamTestServer{})
			rpb.RegisterServerReflectionServer(server, &testReflectionServer{})
			defer server.Stop()
			go server.Serve(ln)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			target := net.JoinHostPort("localhost", fmtPort(ln.Addr()))
			conn, err := grpc.DialContext(ctx, target, upstreamTransport(tc.clientTLS, tc.roots), grpc.WithBlock())
			if tc.fail {
				if err == nil {
					conn.Close()
					t.Fatal("connection succeeded unexpectedly")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			p := proxy.NewProxy(conn)
			if _, err := p.Introspect(); err != nil {
				t.Fatalf("reflection: %v", err)
			}
			md := make(metadata.Metadata)
			if _, err := p.Invoke(ctx, "grpc.testing.TestService", "EmptyCall", []byte("{}"), &md); err != nil {
				t.Fatalf("RPC: %v", err)
			}
		})
	}
}
func fmtPort(addr net.Addr) string { _, port, _ := net.SplitHostPort(addr.String()); return port }

// testReflectionServer serves the vendored test descriptor without adding a
// production dependency on the reflection server package pruned from vendor.
type testReflectionServer struct{}

func (*testReflectionServer) ServerReflectionInfo(stream rpb.ServerReflection_ServerReflectionInfoServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		resp := &rpb.ServerReflectionResponse{OriginalRequest: req}
		switch req.MessageRequest.(type) {
		case *rpb.ServerReflectionRequest_ListServices:
			resp.MessageResponse = &rpb.ServerReflectionResponse_ListServicesResponse{ListServicesResponse: &rpb.ListServiceResponse{Service: []*rpb.ServiceResponse{{Name: "grpc.testing.TestService"}}}}
		default:
			reader, err := gzip.NewReader(bytes.NewReader(proto.FileDescriptor("grpc_testing/test.proto")))
			if err != nil {
				return err
			}
			data, err := ioutil.ReadAll(reader)
			reader.Close()
			if err != nil {
				return err
			}
			resp.MessageResponse = &rpb.ServerReflectionResponse_FileDescriptorResponse{FileDescriptorResponse: &rpb.FileDescriptorResponse{FileDescriptorProto: [][]byte{data}}}
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}
