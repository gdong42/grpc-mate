package proxy

import (
	"context"

	"github.com/gdong42/grpc-mate/metadata"
	"github.com/gdong42/grpc-mate/proxy/reflection"
	"github.com/gdong42/grpc-mate/proxy/stub"
	"github.com/jhump/protoreflect/dynamic/grpcdynamic"
	"github.com/jhump/protoreflect/grpcreflect"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

// Proxy performs upstream invocation as a dynamic gRPC client using reflection
type Proxy struct {
	cc        *grpc.ClientConn
	reflector reflection.Reflector
	stub      stub.Stub
}

// NewProxy creates a new gRPC client
func NewProxy(conn *grpc.ClientConn) *Proxy {
	ctx := context.Background()
	rc := grpcreflect.NewClient(ctx, rpb.NewServerReflectionClient(conn))
	return &Proxy{
		cc:        conn,
		reflector: reflection.NewReflector(rc),
		stub:      stub.NewStub(grpcdynamic.NewStub(conn)),
	}
}

// IsReady checks the connectivity to the upstream
func (p *Proxy) IsReady() bool {
	s := p.cc.GetState()
	return s == connectivity.Ready
}

// Invoke performs the gRPC call after doing reflection to obtain type information
func (p *Proxy) Invoke(ctx context.Context,
	serviceName, methodName string,
	message []byte,
	md *metadata.Metadata,
) ([]byte, error) {
	invocation, err := p.reflector.CreateInvocation(ctx, serviceName, methodName, message)
	if err != nil {
		return nil, err
	}

	outputMsg, err := p.stub.InvokeRPC(ctx, invocation, md)
	if err != nil {
		return nil, err
	}
	m, err := outputMsg.MarshalJSON()
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal output JSON")
	}
	return m, err
}
