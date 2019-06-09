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
	TestService           = "grpc.testing.TestService"
	NotFoundService       = "not.found.NoService"
	EmptyCall             = "EmptyCall"
	UnaryCall             = "UnaryCall"
	UnaryCallInputMsgName = "grpc.testing.SimpleRequest"
	NotFoundCall          = "NotFoundCall"
	File                  = "grpc_testing/test.proto"
	MessageName           = "grpc.testing.Payload"
)

var (
	TestError = errors.Errorf("an error")
)

func NewFileDescriptor(t *testing.T, file string) *desc.FileDescriptor {
	t.Helper()
	desc, err := desc.LoadFileDescriptor(file)
	if err != nil {
		t.Fatal(err.Error())
	}
	return desc
}

type MockGrpcreflectClient struct {
	*desc.FileDescriptor
}

func (c *MockGrpcreflectClient) ResolveService(serviceName string) (*desc.ServiceDescriptor, error) {
	if serviceName != TestService {
		return nil, errors.Errorf("service not found")
	}
	return c.FileDescriptor.FindService(serviceName), nil
}

func (c *MockGrpcreflectClient) ListServices() ([]string, error) {
	sds := c.FileDescriptor.GetServices()
	names := make([]string, len(sds))
	for i, s := range sds {
		names[i] = s.GetName()
	}
	return names, nil
}

type MockGrpcdynamicStub struct {
}

func (m *MockGrpcdynamicStub) InvokeRpc(ctx context.Context, method *desc.MethodDescriptor, request proto.Message, opts ...grpc.CallOption) (proto.Message, error) {
	if method.GetName() == "UnaryCall" {
		return nil, status.Error(codes.Unimplemented, "unary unimplemented")
	}
	output := dynamic.NewMessage(method.GetOutputType())
	return output, nil
}
