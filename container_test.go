package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	pb "google.golang.org/grpc/test/grpc_testing"
)

// TestContainerTLS is opt-in and uses Docker's Linux host network in CI.
func TestContainerTLS(t *testing.T) {
	image := os.Getenv("GRPC_MATE_CONTAINER_IMAGE")
	if image == "" {
		t.Skip("set GRPC_MATE_CONTAINER_IMAGE to test a built runtime image")
	}
	docker := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	}
	cert, _ := testCertificate(t, "localhost")
	caFile := testCAFile(t, cert)
	defer os.Remove(caFile)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}})))
	pb.RegisterTestServiceServer(server, &upstreamTestServer{})
	rpb.RegisterServerReflectionServer(server, &testReflectionServer{})
	defer server.Stop()
	go server.Serve(ln)

	// Reserve an unused HTTP port instead of assuming the runner's 6600 is free.
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := fmtPort(httpLn.Addr())
	httpLn.Close()
	name := fmt.Sprintf("grpc-mate-tls-%d", time.Now().UnixNano())
	defer func() {
		if t.Failed() {
			out, _ := docker("logs", name)
			t.Logf("container logs: %s", out)
		}
		docker("rm", "-f", name)
	}()
	mount := "type=bind,src=" + caFile + ",dst=/certs/ca.pem,readonly"
	out, err := docker("run", "-d", "--name", name, "--network", "host", "--mount", mount,
		"-e", "GRPC_MATE_PORT="+port, "-e", "GRPC_MATE_PROXIED_HOST=localhost",
		"-e", "GRPC_MATE_PROXIED_PORT="+fmtPort(ln.Addr()),
		"-e", "GRPC_MATE_PROXIED_TLS_ENABLED=true", "-e", "GRPC_MATE_PROXIED_TLS_CA_FILE=/certs/ca.pem", image)
	if err != nil {
		t.Fatalf("start container: %v: %s", err, out)
	}
	if out, err := docker("exec", name, "sh", "-c", "test -s /certs/ca.pem && ! echo probe >> /certs/ca.pem"); err != nil {
		t.Fatalf("CA mount must be readable and read-only: %v: %s", err, out)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	base := "http://127.0.0.1:" + port
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := client.Get(base + "/actuator/services")
		if err == nil {
			body, readErr := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK && strings.Contains(string(body), "grpc.testing.TestService") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("TLS reflection through container did not become ready")
		}
		time.Sleep(200 * time.Millisecond)
	}
	resp, err := client.Post(base+"/v1/grpc.testing.TestService/EmptyCall", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("TLS unary RPC: status=%d body=%s err=%v", resp.StatusCode, body, err)
	}

	for _, tc := range []struct {
		name, enabled, path, want string
	}{
		{"CA without TLS", "false", "/certs/ca.pem", "requires GRPC_MATE_PROXIED_TLS_ENABLED=true"},
		{"missing CA", "true", "/certs/missing.pem", "read upstream CA file"},
		{"invalid CA", "true", "/etc/hostname", "no valid PEM certificates"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := docker("run", "--rm", "--mount", mount,
				"-e", "GRPC_MATE_PROXIED_TLS_ENABLED="+tc.enabled,
				"-e", "GRPC_MATE_PROXIED_TLS_CA_FILE="+tc.path, image)
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), tc.want) {
				t.Fatalf("expected startup exit 1 with %q; got %v: %s", tc.want, err, out)
			}
		})
	}
}
