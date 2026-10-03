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
	"encoding/pem"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	old, exists := os.LookupEnv("GRPC_MATE_PROXIED_TLS_ENABLED")
	defer func() {
		if exists {
			os.Setenv("GRPC_MATE_PROXIED_TLS_ENABLED", old)
		} else {
			os.Unsetenv("GRPC_MATE_PROXIED_TLS_ENABLED")
		}
	}()
	for _, tc := range []struct {
		value                string
		unset, want, invalid bool
	}{
		{unset: true}, {value: "false"}, {value: "true", want: true}, {value: "invalid", invalid: true},
	} {
		if tc.unset {
			os.Unsetenv("GRPC_MATE_PROXIED_TLS_ENABLED")
		} else {
			os.Setenv("GRPC_MATE_PROXIED_TLS_ENABLED", tc.value)
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
	privateFile := testCAFile(t, cert)
	defer os.Remove(privateFile)
	privateRoots, err := upstreamRoots(true, privateFile)
	if err != nil {
		t.Fatal(err)
	}
	wrongCert, wrongRoots := testCertificate(t, "wrong.example")
	wrongFile := testCAFile(t, wrongCert)
	defer os.Remove(wrongFile)
	wrongFileRoots, err := upstreamRoots(true, wrongFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                 string
		serverTLS, clientTLS bool
		cert                 tls.Certificate
		roots                *x509.CertPool
		fail                 bool
	}{
		{name: "default plaintext"},
		{name: "private CA file TLS", serverTLS: true, clientTLS: true, cert: cert, roots: privateRoots},
		{name: "private CA file hostname mismatch", serverTLS: true, clientTLS: true, cert: wrongCert, roots: wrongFileRoots, fail: true},
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

func testCAFile(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	f, err := ioutil.TempFile("", "grpc-mate-ca-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[1]}); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// Go 1.12 has no t.Cleanup; the test caller removes these temporary files.
	return path
}

func TestUpstreamRoots(t *testing.T) {
	cert, _ := testCertificate(t, "localhost")
	caFile := testCAFile(t, cert)
	defer os.Remove(caFile)
	invalid, err := ioutil.TempFile("", "grpc-mate-invalid-ca-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalid.WriteString("not a PEM certificate"); err != nil {
		invalid.Close()
		t.Fatal(err)
	}
	if err := invalid.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(invalid.Name())
	empty, err := ioutil.TempFile("", "grpc-mate-empty-ca-*")
	if err != nil {
		t.Fatal(err)
	}
	empty.Close()
	defer os.Remove(empty.Name())
	corrupt, err := ioutil.TempFile("", "grpc-mate-corrupt-ca-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(corrupt, &pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid DER")}); err != nil {
		corrupt.Close()
		t.Fatal(err)
	}
	corrupt.Close()
	defer os.Remove(corrupt.Name())
	for _, tc := range []struct {
		name            string
		enabled         bool
		file, wantError string
	}{
		{name: "default plaintext"},
		{name: "default system roots", enabled: true},
		{name: "CA without TLS", file: caFile, wantError: "requires GRPC_MATE_PROXIED_TLS_ENABLED=true"},
		{name: "missing file", enabled: true, file: caFile + ".missing", wantError: "read upstream CA file"},
		{name: "unreadable directory", enabled: true, file: filepath.Dir(caFile), wantError: "read upstream CA file"},
		{name: "invalid PEM", enabled: true, file: invalid.Name(), wantError: "no valid PEM certificates"},
		{name: "empty bundle", enabled: true, file: empty.Name(), wantError: "no valid PEM certificates"},
		{name: "corrupt certificate", enabled: true, file: corrupt.Name(), wantError: "no valid PEM certificates"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roots, err := upstreamRoots(tc.enabled, tc.file)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error=%v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil || roots != nil {
				t.Fatalf("default roots=%v, error=%v", roots, err)
			}
		})
	}
	system, err := x509.SystemCertPool()
	if err != nil {
		t.Fatal(err)
	}
	combined, err := upstreamRoots(true, caFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(combined.Subjects()) != len(system.Subjects())+1 {
		t.Fatal("custom CA did not append to system roots")
	}
	for i, subject := range system.Subjects() {
		if !bytes.Equal(subject, combined.Subjects()[i]) {
			t.Fatal("system root changed")
		}
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Verify(x509.VerifyOptions{Roots: combined}); err != nil {
		t.Fatalf("appended CA: %v", err)
	}
}

func TestCAFileConfiguration(t *testing.T) {
	const key = "GRPC_MATE_PROXIED_TLS_CA_FILE"
	old, exists := os.LookupEnv(key)
	defer func() {
		if exists {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	}()
	os.Setenv(key, "/certs/company-ca.pem")
	var env EnvConfig
	if err := envconfig.Process("", &env); err != nil {
		t.Fatal(err)
	}
	if env.GrpcServerTLSCAFile != "/certs/company-ca.pem" {
		t.Fatalf("CA path=%q", env.GrpcServerTLSCAFile)
	}
}
