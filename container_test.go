package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gdong42/grpc-mate/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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

// TestContainerPlaintext exercises the shipped routes and plaintext defaults.
func TestContainerPlaintext(t *testing.T) {
	withPlaintextContainer(t, &testReflectionServer{}, func(base string) {
		for _, query := range []string{"", "?name=grpc.testing.TestService", "?method=EmptyCall", "?name=grpc.testing.TestService&method=EmptyCall"} {
			body := containerRequest(t, http.MethodGet, base+"/actuator/services"+query, http.StatusOK)
			var result proxy.IntrospectionResponse
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Services) != 1 || result.Services[0].Name != "grpc.testing.TestService" || len(result.Types) == 0 {
				t.Fatalf("unexpected reflection response for %s: %s", query, body)
			}
			if strings.Contains(query, "method=") {
				methods := result.Services[0].Methods
				if len(methods) != 1 || methods[0].Name != "EmptyCall" || methods[0].Route != "/grpc.testing.TestService/EmptyCall" || len(result.Types) != 1 || result.Types[0].Name != "grpc.testing.Empty" {
					t.Fatalf("filter leaked methods or types: %s", body)
				}
			} else if len(result.Services[0].Methods) < 2 {
				t.Fatalf("unfiltered methods missing: %s", body)
			}
		}
		for _, query := range []string{"?name=missing.Service", "?method=MissingMethod", "?name=missing.Service&method=EmptyCall", "?name=grpc.testing.testservice", "?method=emptycall"} {
			body := containerRequest(t, http.MethodGet, base+"/actuator/services"+query, http.StatusOK)
			var result map[string]json.RawMessage
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			if string(result["services"]) != "[]" || string(result["types"]) != "[]" {
				t.Fatalf("no-match %s must return empty arrays: %s", query, body)
			}
		}
		body := containerRequest(t, http.MethodPost, base+"/v1/grpc.testing.TestService/EmptyCall", http.StatusOK)
		if strings.TrimSpace(string(body)) != "{}" {
			t.Fatalf("plaintext unary RPC: %s", body)
		}
		for _, tc := range []struct {
			path, message string
			status        int
		}{
			{"/v1/missing.Service/EmptyCall", "service missing.Service was not found upstream", http.StatusInternalServerError},
			{"/v1/grpc.testing.TestService/MissingMethod", "the method MissingMethod was not found", http.StatusNotFound},
			{"/v1/grpc.testing.TestService/UnaryCall", "application method unimplemented", http.StatusNotImplemented},
		} {
			checkContainerError(t, containerRequest(t, http.MethodPost, base+tc.path, tc.status), tc.message)
		}
	})
}

func TestContainerReflectionErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reflection rpb.ServerReflectionServer
		status     int
		message    string
	}{
		{"not registered", nil, http.StatusBadGateway, "enable server reflection"},
		{"unimplemented", &failingReflectionServer{code: codes.Unimplemented}, http.StatusBadGateway, "enable server reflection"},
		{"permission denied", &failingReflectionServer{code: codes.PermissionDenied}, http.StatusForbidden, "reflection access denied"},
		{"unavailable", &failingReflectionServer{code: codes.Unavailable}, http.StatusBadGateway, "could not connect to backend"},
		{"deadline exceeded", &failingReflectionServer{code: codes.DeadlineExceeded}, http.StatusRequestTimeout, "reflection access denied"},
		{"descriptor denied", &failingReflectionServer{code: codes.PermissionDenied, listOK: true}, http.StatusForbidden, "reflection access denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPlaintextContainer(t, tc.reflection, func(base string) {
				for _, endpoint := range []struct{ method, path string }{{http.MethodGet, "/actuator/services"}, {http.MethodPost, "/v1/grpc.testing.TestService/EmptyCall"}} {
					checkContainerError(t, containerRequest(t, endpoint.method, base+endpoint.path, tc.status), tc.message)
				}
			})
		})
	}
}

func checkContainerError(t *testing.T, body []byte, want string) {
	t.Helper()
	var result struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Message, want) || (want != "enable server reflection" && strings.Contains(result.Message, "enable server reflection")) {
		t.Fatalf("error message=%q, want %q", result.Message, want)
	}
}

func containerRequest(t *testing.T, method, url string, wantStatus int) []byte {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: status=%d, want %d, body=%s, err=%v", method, url, resp.StatusCode, wantStatus, body, err)
	}
	return body
}

func withPlaintextContainer(t *testing.T, reflection rpb.ServerReflectionServer, check func(string)) {
	t.Helper()
	image := os.Getenv("GRPC_MATE_CONTAINER_IMAGE")
	if image == "" {
		t.Skip("set GRPC_MATE_CONTAINER_IMAGE to test a built runtime image")
	}
	docker := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterTestServiceServer(server, &reflectionHTTPTestServer{})
	if reflection != nil {
		rpb.RegisterServerReflectionServer(server, reflection)
	}
	defer server.Stop()
	go server.Serve(ln)
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := fmtPort(httpLn.Addr())
	httpLn.Close()
	name := fmt.Sprintf("grpc-mate-smoke-%d", time.Now().UnixNano())
	defer func() {
		if t.Failed() {
			out, _ := docker("logs", name)
			t.Logf("container logs: %s", out)
		}
		if out, err := docker("rm", "-f", name); err != nil {
			t.Errorf("remove container: %v: %s", err, out)
		}
	}()
	// Deliberately omit TLS environment variables to test plaintext defaults.
	out, err := docker("run", "-d", "--name", name, "--network", "host", "-e", "GRPC_MATE_PORT="+port, "-e", "GRPC_MATE_PROXIED_HOST=127.0.0.1", "-e", "GRPC_MATE_PROXIED_PORT="+fmtPort(ln.Addr()), image)
	if err != nil {
		t.Fatalf("start container: %v: %s", err, out)
	}
	base := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	var lastBody []byte
	for {
		resp, err := client.Get(base + "/actuator/services")
		if err == nil {
			body, readErr := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			lastBody = body
			// IsReady returns an empty 502 before connecting to the upstream.
			// A JSON response, even a reflection error, proves that it connected.
			if readErr == nil && json.Valid(body) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("container did not become ready: err=%v body=%s", err, lastBody)
		}
		time.Sleep(200 * time.Millisecond)
	}
	check(base)
}
