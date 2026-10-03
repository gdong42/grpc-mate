package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	matehttp "github.com/gdong42/grpc-mate/http"
	"github.com/gdong42/grpc-mate/proxy"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	pb "google.golang.org/grpc/test/grpc_testing"
)

// Exercise the real reflection client through both HTTP entry points, including
// servers with no reflection service and failures carried inside its stream.
func TestReflectionHTTPDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name            string
		reflection      rpb.ServerReflectionServer
		service, method string
		wantStatus      int
		wantMessage     string
		rpcOnly         bool
	}{
		{name: "reflection not registered", wantStatus: http.StatusBadGateway, wantMessage: "enable server reflection"},
		{name: "reflection unimplemented", reflection: &failingReflectionServer{code: codes.Unimplemented}, wantStatus: http.StatusBadGateway, wantMessage: "enable server reflection"},
		{name: "reflection permission denied", reflection: &failingReflectionServer{code: codes.PermissionDenied}, wantStatus: http.StatusForbidden, wantMessage: "reflection access denied"},
		{name: "reflection connection unavailable", reflection: &failingReflectionServer{code: codes.Unavailable}, wantStatus: http.StatusBadGateway, wantMessage: "could not connect to backend"},
		{name: "reflection deadline exceeded", reflection: &failingReflectionServer{code: codes.DeadlineExceeded}, wantStatus: http.StatusRequestTimeout, wantMessage: "reflection access denied"},
		{name: "descriptor permission denied", reflection: &failingReflectionServer{code: codes.PermissionDenied, listOK: true}, wantStatus: http.StatusForbidden, wantMessage: "reflection access denied"},
		{name: "descriptor unimplemented", reflection: &failingReflectionServer{code: codes.Unimplemented, listOK: true}, wantStatus: http.StatusBadGateway, wantMessage: "enable server reflection"},
		{name: "service missing", reflection: &testReflectionServer{}, service: "missing.Service", wantStatus: http.StatusInternalServerError, wantMessage: "service missing.Service was not found upstream", rpcOnly: true},
		{name: "method missing", reflection: &testReflectionServer{}, method: "MissingMethod", wantStatus: http.StatusNotFound, wantMessage: "the method MissingMethod was not found", rpcOnly: true},
		{name: "application unimplemented", reflection: &testReflectionServer{}, method: "UnaryCall", wantStatus: http.StatusNotImplemented, wantMessage: "application method unimplemented", rpcOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := grpc.NewServer()
			pb.RegisterTestServiceServer(server, &reflectionHTTPTestServer{})
			if tc.reflection != nil {
				rpb.RegisterServerReflectionServer(server, tc.reflection)
			}
			defer server.Stop()
			go server.Serve(ln)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn, err := grpc.DialContext(ctx, ln.Addr().String(), grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			service, method := tc.service, tc.method
			if service == "" {
				service = "grpc.testing.TestService"
			}
			if method == "" {
				method = "EmptyCall"
			}
			endpoints := []struct{ method, path string }{{http.MethodPost, "/v1/" + service + "/" + method}}
			if !tc.rpcOnly {
				endpoints = append(endpoints, struct{ method, path string }{http.MethodGet, "/actuator/services"})
			}
			for _, endpoint := range endpoints {
				t.Run(endpoint.path, func(t *testing.T) {
					// Use a fresh proxy so descriptor caches cannot mask reflection failures.
					p := proxy.NewProxy(conn)
					s := matehttp.New(p, zap.NewNop())
					handler := s.RPCCallHandler(p)
					if endpoint.method == http.MethodGet {
						handler = s.IntrospectHandler(p)
					}
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader("{}")))
					if rec.Code != tc.wantStatus {
						t.Fatalf("HTTP status=%d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
					}
					var body struct {
						Message string `json:"message"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(body.Message, tc.wantMessage) {
						t.Fatalf("message=%q, want %q", body.Message, tc.wantMessage)
					}
					if tc.wantStatus != http.StatusBadGateway && strings.Contains(body.Message, "enable server reflection") {
						t.Fatalf("misleading reflection guidance: %q", body.Message)
					}
				})
			}
		})
	}
}

type reflectionHTTPTestServer struct{ upstreamTestServer }

func (*reflectionHTTPTestServer) UnaryCall(context.Context, *pb.SimpleRequest) (*pb.SimpleResponse, error) {
	return nil, status.Error(codes.Unimplemented, "application method unimplemented")
}

type failingReflectionServer struct {
	code   codes.Code
	listOK bool
}

func (s *failingReflectionServer) ServerReflectionInfo(stream rpb.ServerReflection_ServerReflectionInfoServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		resp := &rpb.ServerReflectionResponse{OriginalRequest: req}
		if _, ok := req.MessageRequest.(*rpb.ServerReflectionRequest_ListServices); ok && s.listOK {
			resp.MessageResponse = &rpb.ServerReflectionResponse_ListServicesResponse{ListServicesResponse: &rpb.ListServiceResponse{Service: []*rpb.ServiceResponse{{Name: "grpc.testing.TestService"}}}}
		} else {
			resp.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{ErrorResponse: &rpb.ErrorResponse{ErrorCode: int32(s.code), ErrorMessage: "reflection access denied"}}
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}
