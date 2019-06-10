package proxy

import (
	"context"
	"encoding/json"

	"github.com/fullstorydev/grpcurl"
	perrors "github.com/gdong42/grpc-mate/errors"
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
	cc         *grpc.ClientConn
	reflector  reflection.Reflector
	stub       stub.Stub
	descSource grpcurl.DescriptorSource
}

// NewProxy creates a new gRPC client
func NewProxy(conn *grpc.ClientConn) *Proxy {
	ctx := context.Background()
	rc := grpcreflect.NewClient(ctx, rpb.NewServerReflectionClient(conn))
	return &Proxy{
		cc:         conn,
		reflector:  reflection.NewReflector(rc),
		stub:       stub.NewStub(grpcdynamic.NewStub(conn)),
		descSource: grpcurl.DescriptorSourceFromServer(ctx, rc),
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
	invocation, err := p.reflector.CreateInvocation(serviceName, methodName, message)
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

// Introspect performs instrospection on this gRPC server, and obtains all services and methods
// infromation
func (p *Proxy) Introspect() ([]byte, error) {
	if !p.IsReady() {
		return nil, &perrors.ProxyError{
			Code:    perrors.UpstreamConnFailure,
			Message: "service down",
		}
	}
	s, err := p.reflector.ListServices()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list services")
	}
	ses := make([]*serviceElement, len(s))
	// typeDscs holds a message name to MessageDescriptor mappings without duplicates
	typeDscs := make(map[string]*reflection.MessageDescriptor)
	r := &IntrospectionResponse{
		Services: ses,
	}
	for i, svc := range s {
		mds, err := p.reflector.DescribeService(svc)
		if err != nil {
			return nil, err
		}
		methods := make([]*methodElement, len(mds))
		for j, m := range mds {
			methods[j] = resolveMethodElement(svc, m, typeDscs)
		}
		se := &serviceElement{
			Name:    svc,
			Methods: methods,
		}
		r.Services[i] = se
	}

	var types []*typeElement
	for k, v := range typeDscs {
		te, err := resolveTypeElement(k, v, p.descSource)
		if err != nil {
			return nil, errors.Wrap(err, "failed to resolve type "+k)
		}
		types = append(types, te)
	}
	r.Types = types
	js, err := json.Marshal(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal output JSON")
	}
	return js, nil
}

func resolveTypeElement(typeName string, md *reflection.MessageDescriptor,
	descSource grpcurl.DescriptorSource) (*typeElement, error) {

	tmpl, err := md.MakeTemplate(descSource)
	if err != nil {
		return nil, err
	}
	var t *json.RawMessage
	err = json.Unmarshal([]byte(tmpl), &t)
	return &typeElement{
		Name:     typeName,
		Template: t,
	}, err
}

func resolveMethodElement(svc string, md *reflection.MethodDescriptor,
	types map[string]*reflection.MessageDescriptor) *methodElement {

	inType := md.GetInputType()
	outType := md.GetOutputType()
	types[inType.GetFullyQualifiedName()] = inType
	types[outType.GetFullyQualifiedName()] = outType
	return &methodElement{
		Name:       md.GetName(),
		InputType:  inType.GetFullyQualifiedName(),
		OutputType: outType.GetFullyQualifiedName(),
		Route:      "/" + svc + "/" + md.GetName(),
	}
}

// IntrospectionResponse represents a introspection response
type IntrospectionResponse struct {
	Services []*serviceElement `json:"services"`
	Types    []*typeElement    `json:"types"`
}

type serviceElement struct {
	Name    string           `json:"name"`
	Methods []*methodElement `json:"methods"`
}

type methodElement struct {
	Name       string `json:"name"`
	InputType  string `json:"input"`
	OutputType string `json:"output"`
	Route      string `json:"route"`
}

type typeElement struct {
	Name     string           `json:"name"`
	Template *json.RawMessage `json:"template"`
}
