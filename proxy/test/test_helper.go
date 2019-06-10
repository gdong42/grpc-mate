package test

import (
	"context"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// TestService is an example service
	TestService = "grpc.testing.TestService"
	// NotFoundService is a service name that does not exist
	NotFoundService = "not.found.NoService"
	// EmptyCall is a method name
	EmptyCall = "EmptyCall"
	// UnaryCall is a method name
	UnaryCall = "UnaryCall"
	// UnaryCallInputMsgName is the type name of method UnaryCall
	UnaryCallInputMsgName = "grpc.testing.SimpleRequest"
	// NotFoundCall is a method name that does not exist
	NotFoundCall = "NotFoundCall"
	// File is s example protobuf file
	File = "grpc_testing/test.proto"
	// MessageName is another message type name
	MessageName = "grpc.testing.Payload"
)

var (
	// TestError ...
	TestError = errors.Errorf("an error")
)

// NewFileDescriptor creates a FileDescriptor from the given file for testing
func NewFileDescriptor(t *testing.T, file string) *desc.FileDescriptor {
	t.Helper()
	desc, err := desc.LoadFileDescriptor(file)
	if err != nil {
		t.Fatal(err.Error())
	}
	return desc
}

// MockGrpcreflectClient is a mock of grpcreflectClient
type MockGrpcreflectClient struct {
	*desc.FileDescriptor
}

// ResolveService is a mock that returns TestService from test.proto
func (c *MockGrpcreflectClient) ResolveService(serviceName string) (*desc.ServiceDescriptor, error) {
	if serviceName != TestService {
		return nil, errors.Errorf("service not found")
	}
	return c.FileDescriptor.FindService(serviceName), nil
}

// ListServices is a mock that returns all services from test.proto
func (c *MockGrpcreflectClient) ListServices() ([]string, error) {
	sds := c.FileDescriptor.GetServices()
	names := make([]string, len(sds))
	for i, s := range sds {
		names[i] = s.GetName()
	}
	return names, nil
}

// MockGrpcdynamicStub is a mock of grpcdynamicStub
type MockGrpcdynamicStub struct {
}

// InvokeRpc mocks the invocation of an RPC call
func (m *MockGrpcdynamicStub) InvokeRpc(ctx context.Context, method *desc.MethodDescriptor, request proto.Message, opts ...grpc.CallOption) (proto.Message, error) {
	if method.GetName() == "UnaryCall" {
		return nil, status.Error(codes.Unimplemented, "unary unimplemented")
	}
	output := dynamic.NewMessage(method.GetOutputType())
	return output, nil
}
