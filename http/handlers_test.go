package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	perrors "github.com/gdong42/grpc-mate/errors"
	"github.com/gdong42/grpc-mate/metadata"
	any "github.com/golang/protobuf/ptypes/any"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
)

type mockClient struct {
	isReady   bool
	invokeErr error
}

func (c *mockClient) IsReady() bool {
	return c.isReady
}

func (c *mockClient) Invoke(ctx context.Context,
	serviceName string,
	methodName string,
	message []byte,
	md *metadata.Metadata,
) ([]byte, error) {
	if c.invokeErr != nil {
		return nil, c.invokeErr
	}
	response := fmt.Sprintf(`{"service":"%s","method":"%s"}`,
		serviceName,
		methodName)
	return []byte(response), nil
}

func (c *mockClient) Introspect() ([]byte, error) {
	response := `{"services":[{
		"name": "helloworld.Greeter",
		"methods": []
	}],"types":[]}`
	return []byte(response), nil
}

func TestHealthCheckHandler(t *testing.T) {
	mc := &mockClient{}
	server := New(mc, zap.NewNop())

	req, err := http.NewRequest("GET", "/actuator/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HealthCheckHandler())

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	if ctype := rr.Header().Get("Content-Type"); ctype != "application/json" {
		t.Errorf("content type header does not match: got %v want %v",
			ctype, "application/json")
	}
	expected := `{"status":"UP"}`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func TestIntrospectHandler(t *testing.T) {
	mc := &mockClient{
		isReady: true,
	}
	server := New(mc, zap.NewNop())

	req, err := http.NewRequest("GET", "/actuator/services", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.IntrospectHandler(mc))

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	if ctype := rr.Header().Get("Content-Type"); ctype != "application/json" {
		t.Errorf("content type header does not match: got %v want %v",
			ctype, "application/json")
	}

	var actual map[string]*json.RawMessage
	err = json.Unmarshal(rr.Body.Bytes(), &actual)
	if err != nil {
		t.Errorf("Invalid JSON in response: %s, err: %v", rr.Body.String(), err)
	}
	if _, ok := actual["services"]; !ok {
		t.Errorf("handler did not returns expected body key: services, got %v", actual)
	}
	if _, ok := actual["types"]; !ok {
		t.Errorf("handler did not returns expected body key: types, got %v", actual)
	}
}

func TestCatchAllHandler(t *testing.T) {
	mc := &mockClient{}
	server := New(mc, zap.NewNop())

	req, err := http.NewRequest("GET", "/path-does-not-exist", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.CatchAllHandler())

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNotFound)
	}
}

func TestRPCCallHandlerOnlyAcceptsPost(t *testing.T) {
	mc := &mockClient{
		isReady: true,
	}
	server := New(mc, zap.NewNop())

	// test that GET requests should return 405(MethodNotAllowed)
	req, err := http.NewRequest("GET", "/v1/svc1/method1", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.RPCCallHandler(mc))

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusMethodNotAllowed)
	}
}

func TestRPCCallHandlerReturns502WhenUpstreamClientIsNotAvailable(t *testing.T) {
	// test that 502(BadGateway) is returned when upstream client is not ready
	mc := &mockClient{
		isReady: false,
	}
	server := New(mc, zap.NewNop())
	req, err := http.NewRequest("POST", "/v1/svc1/method1", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.RPCCallHandler(mc))

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadGateway {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusBadGateway)
	}
}

func TestRPCCallHandlerSuccess(t *testing.T) {
	mc := &mockClient{
		isReady: true,
	}
	server := New(mc, zap.NewNop())
	req, err := http.NewRequest("POST", "/v1/svc1/method1",
		strings.NewReader(`{"foo":42,"hello":"gdong42"}`))
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.RPCCallHandler(mc))

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	if ctype := rr.Header().Get("Content-Type"); ctype != "application/json" {
		t.Errorf("content type header does not match: got %v want %v",
			ctype, "application/json")
	}
	var actual map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &actual)
	if err != nil {
		t.Errorf("Invalid JSON in response: %s, err: %v", rr.Body.String(), err)
	}
	actualSvc, ok := actual["service"]
	if !ok || actualSvc != "svc1" {
		t.Errorf("handler did not returns expected value [svc1] for body key: service, got %v", actualSvc)
	}
	actualMethod, ok := actual["method"]
	if !ok || actualMethod != "method1" {
		t.Errorf("handler did not returns expected value [method1] for body key: method, got %v", actualMethod)
	}
}

func TestRPCCallHandlerGRPCErrors(t *testing.T) {
	cases := []struct {
		code       codes.Code
		httpStatus int
	}{
		{codes.ResourceExhausted, http.StatusTooManyRequests},
		{codes.Unavailable, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.code.String(), func(t *testing.T) {
			upstreamErr := &perrors.GRPCError{
				StatusCode: int(tc.code),
				Message:    "upstream limit reached",
				Details:    []*any.Any{{TypeUrl: "type.googleapis.com/google.rpc.RetryInfo", Value: []byte{8, 1}}},
			}
			client := &mockClient{isReady: true, invokeErr: errors.Wrap(upstreamErr, "invoke failed")}
			server := New(client, zap.NewNop())
			req := httptest.NewRequest(http.MethodPost, "/v1/svc1/method1", strings.NewReader(`{}`))
			rr := httptest.NewRecorder()
			server.RPCCallHandler(client).ServeHTTP(rr, req)
			if rr.Code != tc.httpStatus {
				t.Errorf("HTTP status: got %d, want %d", rr.Code, tc.httpStatus)
			}
			var actual perrors.GRPCError
			if err := json.Unmarshal(rr.Body.Bytes(), &actual); err != nil {
				t.Fatalf("invalid JSON response: %v", err)
			}
			if !reflect.DeepEqual(&actual, upstreamErr) {
				t.Errorf("gRPC error body: got %+v, want %+v", &actual, upstreamErr)
			}
		})
	}
}
