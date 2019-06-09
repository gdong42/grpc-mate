package proxy

import (
	"context"
	"testing"

	"github.com/gdong42/grpc-mate/metadata"
	"github.com/gdong42/grpc-mate/proxy/reflection"
	"github.com/gdong42/grpc-mate/proxy/stub"
	"github.com/gdong42/grpc-mate/proxy/test"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/test/grpc_testing"
)

func TestNewProxy(t *testing.T) {
	cc, err := grpc.Dial("localhost:5000", grpc.WithInsecure())
	if err != nil {
		t.Fatal(err.Error())
	}
	p := NewProxy(cc)
	if p == nil {
		t.Fatalf("proxy was nil")
	}
}

func TestIsReady(t *testing.T) {
	cc, err := grpc.Dial("localhost:5000", grpc.WithInsecure())
	if err != nil {
		t.Fatal(err.Error())
	}
	p := NewProxy(cc)
	if got, want := p.IsReady(), false; got != want {
		t.Fatalf("got %t, want %t", got, want)
	}
}

func TestInvoke(t *testing.T) {
	cc, err := grpc.Dial("localhost:5000", grpc.WithInsecure())
	if err != nil {
		t.Fatal(err.Error())
	}
	t.Run("success", func(t *testing.T) {
		p := NewProxy(cc)
		ctx := context.Background()
		md := make(metadata.Metadata)

		p.stub = stub.NewStub(&test.MockGrpcdynamicStub{})
		fd := test.NewFileDescriptor(t, test.File)
		p.reflector = reflection.NewReflector(&test.MockGrpcreflectClient{FileDescriptor: fd})

		_, err := p.Invoke(ctx, test.TestService, test.EmptyCall, []byte("{}"), &md)
		if err != nil {
			t.Fatalf("err should be nil, got %s", err.Error())
		}
	})

	t.Run("reflector fails", func(t *testing.T) {
		p := NewProxy(cc)
		ctx := context.Background()
		md := make(metadata.Metadata)

		p.stub = stub.NewStub(&test.MockGrpcdynamicStub{})
		p.reflector = reflection.NewReflector(&test.MockGrpcreflectClient{})

		_, err := p.Invoke(ctx, test.NotFoundService, test.EmptyCall, []byte("{}"), &md)
		if err == nil {
			t.Fatalf("err should be not nil")
		}
	})

	t.Run("invoking RPC returns error", func(t *testing.T) {
		p := NewProxy(cc)
		ctx := context.Background()
		md := make(metadata.Metadata)

		p.stub = stub.NewStub(&test.MockGrpcdynamicStub{})
		fd := test.NewFileDescriptor(t, test.File)
		p.reflector = reflection.NewReflector(&test.MockGrpcreflectClient{FileDescriptor: fd})

		_, err := p.Invoke(ctx, test.TestService, test.UnaryCall, []byte("{}"), &md)
		if err == nil {
			t.Fatalf("err should be not nil")
		}
	})
}

func TestIntrospect(t *testing.T) {
	cc, err := grpc.Dial("localhost:5000", grpc.WithInsecure())
	if err != nil {
		t.Fatal(err.Error())
	}
	p := NewProxy(cc)

	p.stub = stub.NewStub(&test.MockGrpcdynamicStub{})
	fd := test.NewFileDescriptor(t, test.File)
	p.reflector = reflection.NewReflector(&test.MockGrpcreflectClient{FileDescriptor: fd})

	_, err = p.Introspect()
	if err == nil {
		t.Fatalf("err should not be nil, got %s", err.Error())
	}
}
