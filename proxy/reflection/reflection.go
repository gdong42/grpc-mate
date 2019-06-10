package reflection

import (
	"fmt"

	"github.com/fullstorydev/grpcurl"
	"github.com/golang/protobuf/proto"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/pkg/errors"

	perrors "github.com/gdong42/grpc-mate/errors"
)

// MethodInvocation contains a method and a message used to invoke an RPC
type MethodInvocation struct {
	*MethodDescriptor
	Message
}

// Reflector performs reflection on the gRPC service to obtain the method
// type, services and methods
type Reflector interface {
	CreateInvocation(serviceName, methodName string, input []byte) (*MethodInvocation, error)
	ListServices() ([]string, error)
	DescribeService(serviceName string) ([]*MethodDescriptor, error)
}

// NewReflector creates a new Reflector from the reflection client
func NewReflector(rc grpcreflectClient) Reflector {
	return &reflectorImpl{
		rc: newReflectionClient(rc),
	}
}

type reflectorImpl struct {
	rc *reflectionClient
}

// CreateInvocation creates a MethodInvocation by performing reflection
func (r *reflectorImpl) CreateInvocation(serviceName,
	methodName string,
	input []byte,
) (*MethodInvocation, error) {
	serviceDesc, err := r.rc.resolveService(serviceName)
	if err != nil {
		return nil, errors.Wrap(err, "service was not found upstream even though it should have been there")
	}
	methodDesc, err := serviceDesc.FindMethodByName(methodName)
	if err != nil {
		return nil, errors.Wrap(err, "method not found upstream")
	}
	inputMessage := methodDesc.GetInputType().NewMessage()
	err = inputMessage.UnmarshalJSON(input)
	if err != nil {
		return nil, err
	}
	return &MethodInvocation{
		MethodDescriptor: methodDesc,
		Message:          inputMessage,
	}, nil
}

func (r *reflectorImpl) ListServices() ([]string, error) {
	return r.rc.listServices()
}

func (r *reflectorImpl) DescribeService(serviceName string) ([]*MethodDescriptor, error) {
	serviceDesc, err := r.rc.resolveService(serviceName)
	if err != nil {
		return nil, errors.Wrap(err, "service was not found upstream even though it should have been there")
	}
	methodDescs, err := serviceDesc.GetMethods()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get methods from service "+serviceName)
	}
	return methodDescs, nil
}

// reflectionClient performs reflection to obtain descriptors
type reflectionClient struct {
	grpcreflectClient
}

// grpcreflectClient is a super type of grpcreflect.Client
type grpcreflectClient interface {
	ResolveService(serviceName string) (*desc.ServiceDescriptor, error)
	ListServices() ([]string, error)
}

// newReflectionClient creates a new ReflectionClient
func newReflectionClient(rc grpcreflectClient) *reflectionClient {
	return &reflectionClient{
		grpcreflectClient: rc,
	}
}

func (c *reflectionClient) resolveService(serviceName string) (*ServiceDescriptor, error) {
	d, err := c.grpcreflectClient.ResolveService(serviceName)
	if err != nil {
		return nil, &perrors.ProxyError{
			Code:    perrors.ServiceNotFound,
			Message: fmt.Sprintf("service %s was not found upstream", serviceName),
		}
	}
	return &ServiceDescriptor{
		ServiceDescriptor: d,
	}, nil
}

func (c *reflectionClient) listServices() ([]string, error) {
	d, err := c.grpcreflectClient.ListServices()
	if err != nil {
		return nil, &perrors.ProxyError{
			Code:    perrors.ServiceNotFound,
			Message: fmt.Sprintf("listing service failed: %v", err),
		}
	}
	return d, nil
}

// ServiceDescriptor represents a service type
type ServiceDescriptor struct {
	*desc.ServiceDescriptor
}

// ServiceDescriptorFromFileDescriptor finds the service descriptor from a file descriptor
// This can be useful in tests that don't connect to a real server
func ServiceDescriptorFromFileDescriptor(fd *desc.FileDescriptor, service string) *ServiceDescriptor {
	d := fd.FindService(service)
	if d == nil {
		return nil
	}
	return &ServiceDescriptor{
		ServiceDescriptor: d,
	}
}

// GetMethods returns all of the RPC methods of this service
func (s *ServiceDescriptor) GetMethods() ([]*MethodDescriptor, error) {
	methods := s.ServiceDescriptor.GetMethods()
	ret := make([]*MethodDescriptor, len(methods))
	for i, m := range methods {
		ret[i] = &MethodDescriptor{
			MethodDescriptor: m,
		}
	}
	return ret, nil
}

// FindMethodByName finds the method descriptor by name from the service descriptor
func (s *ServiceDescriptor) FindMethodByName(name string) (*MethodDescriptor, error) {
	d := s.ServiceDescriptor.FindMethodByName(name)
	if d == nil {
		return nil, &perrors.ProxyError{
			Code:    perrors.MethodNotFound,
			Message: fmt.Sprintf("the method %s was not found", name),
		}
	}
	return &MethodDescriptor{
		MethodDescriptor: d,
	}, nil
}

// MethodDescriptor represents a method type
type MethodDescriptor struct {
	*desc.MethodDescriptor
}

// GetInputType gets the MessageDescriptor for the method input type
func (m *MethodDescriptor) GetInputType() *MessageDescriptor {
	return &MessageDescriptor{
		desc: m.MethodDescriptor.GetInputType(),
	}
}

// GetOutputType gets the MessageDescriptor for the method output type
func (m *MethodDescriptor) GetOutputType() *MessageDescriptor {
	return &MessageDescriptor{
		desc: m.MethodDescriptor.GetOutputType(),
	}
}

// AsProtoreflectDescriptor returns the underlying protoreflect method descriptor
func (m *MethodDescriptor) AsProtoreflectDescriptor() *desc.MethodDescriptor {
	return m.MethodDescriptor
}

// GetName returns the name of the method.
func (m *MethodDescriptor) GetName() string {
	return m.MethodDescriptor.GetName()
}

// MessageDescriptor represents a message type
type MessageDescriptor struct {
	desc *desc.MessageDescriptor
}

// NewMessage creates a new message from the message descriptor
func (m *MessageDescriptor) NewMessage() *messageImpl {
	return &messageImpl{
		Message: dynamic.NewMessage(m.desc),
	}
}

// GetFullyQualifiedName returns the fully qualified name of the underlying message
func (m *MessageDescriptor) GetFullyQualifiedName() string {
	return m.desc.GetFullyQualifiedName()
}

// MakeTemplateMessage makes a message template for this message, to make it easier to
// create a request to invoke an RPC
func (m *MessageDescriptor) MakeTemplateMessage(descSource grpcurl.DescriptorSource) proto.Message {
	return grpcurl.MakeTemplate(m.desc)
}

// MakeTemplate makes a JSON template for this message, to make it easier to
// create a request to invoke an RPC
func (m *MessageDescriptor) MakeTemplate(descSource grpcurl.DescriptorSource) (string, error) {
	tmpl := grpcurl.MakeTemplate(m.desc)
	_, formatter, err := grpcurl.RequestParserAndFormatterFor(grpcurl.FormatJSON, descSource, true, false, nil)
	if err != nil {
		return "", &perrors.ProxyError{
			Code:    perrors.Unknown,
			Message: "Failed to construct formatter for JSON",
		}
	}
	str, err := formatter(tmpl)
	if err != nil {
		return "", &perrors.ProxyError{
			Code:    perrors.Unknown,
			Message: "Failed to print template for message: " + m.GetFullyQualifiedName(),
		}
	}
	return str, err
}

// Message is an simple abstraction of protobuf message
type Message interface {
	// MarshalJSON marshals the Message into JSON
	MarshalJSON() ([]byte, error)
	// UnmarshalJSON unmarshals JSON into a Message
	UnmarshalJSON(b []byte) error
	// ConvertFrom converts a raw protobuf message into a Message
	ConvertFrom(target proto.Message) error
	// AsProtoreflectMessage returns the underlying protoreflect message
	AsProtoreflectMessage() *dynamic.Message
}

// messageImpl is an message value
type messageImpl struct {
	*dynamic.Message
}

func (m *messageImpl) MarshalJSON() ([]byte, error) {
	b, err := m.Message.MarshalJSON()
	if err != nil {
		return nil, &perrors.ProxyError{
			Code:    perrors.Unknown,
			Message: "could not marshal backend response into JSON",
		}
	}
	return b, nil
}

func (m *messageImpl) UnmarshalJSON(b []byte) error {
	if err := m.Message.UnmarshalJSON(b); err != nil {
		return &perrors.ProxyError{
			Code:    perrors.MessageTypeMismatch,
			Message: "input JSON does not match messageImpl type",
		}
	}
	return nil
}

func (m *messageImpl) ConvertFrom(target proto.Message) error {
	return m.Message.ConvertFrom(target)
}

func (m *messageImpl) AsProtoreflectMessage() *dynamic.Message {
	return m.Message
}
