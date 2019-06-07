package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gdong42/grpc-mate/metadata"
	"go.uber.org/zap"
)

type mockClient struct {
	isReady bool
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
