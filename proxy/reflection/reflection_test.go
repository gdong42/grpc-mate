package reflection

import (
	"fmt"
	"reflect"
	"testing"

	perrors "github.com/gdong42/grpc-mate/errors"
	"github.com/gdong42/grpc-mate/proxy/test"
	"github.com/jhump/protoreflect/dynamic"
	_ "google.golang.org/grpc/test/grpc_testing"
)

func TestNewReflector(t *testing.T) {
	r := NewReflector(&test.MockGrpcreflectClient{})
	if r == nil {
		t.Fatal("reflector should not be nil")
	}
}

func TestReflectorImpl_CreateInvocation(t *testing.T) {
	cases := []struct {
		name            string
		serviceName     string
		methodName      string
		message         []byte
		invocationIsNil bool
		errorIsNil      bool
	}{
		{
			name:            "found",
			serviceName:     test.TestService,
			methodName:      test.EmptyCall,
			message:         []byte("{}"),
			invocationIsNil: false,
			errorIsNil:      true,
		},
		{
			name:            "service not found",
			serviceName:     test.NotFoundService,
			methodName:      test.EmptyCall,
			message:         []byte("{}"),
			invocationIsNil: true,
			errorIsNil:      false,
		},
		{
			name:            "method not found",
			serviceName:     test.TestService,
			methodName:      test.NotFoundCall,
			message:         []byte("{}"),
			invocationIsNil: true,
			errorIsNil:      false,
		},
		{
			name:            "unmarshal failed",
			serviceName:     test.TestService,
			methodName:      test.EmptyCall,
			message:         []byte("{"),
			invocationIsNil: true,
			errorIsNil:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fd := test.NewFileDescriptor(t, test.File)
			r := NewReflector(&test.MockGrpcreflectClient{FileDescriptor: fd})
			i, err := r.CreateInvocation(tc.serviceName, tc.methodName, []byte(tc.message))
			if got, want := i == nil, tc.invocationIsNil; got != want {
				t.Fatalf("got %t, want %t", got, want)
			}
			if got, want := err == nil, tc.errorIsNil; got != want {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestReflectorImpl_ListServices(t *testing.T) {
	fd := test.NewFileDescriptor(t, test.File)
	r := NewReflector(&test.MockGrpcreflectClient{FileDescriptor: fd})
	services, err := r.ListServices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(services), 1; got != want {
		t.Fatalf("got %d services, want %d", got, want)
	}
}

func TestReflectorImpl_DescribeService(t *testing.T) {
	cases := []struct {
		name        string
		serviceName string
		methodsLen  int
		descIsNil   bool
		errorIsNil  bool
	}{
		{
			name:        "found",
			serviceName: test.TestService,
			methodsLen:  6,
			descIsNil:   false,
			errorIsNil:  true,
		},
		{
			name:        "not found",
			serviceName: test.NotFoundService,
			methodsLen:  0,
			descIsNil:   true,
			errorIsNil:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReflector(&test.MockGrpcreflectClient{FileDescriptor: test.NewFileDescriptor(t, test.File)})
			methodDescs, err := r.DescribeService(tc.serviceName)

			if got, want := len(methodDescs), tc.methodsLen; got != want {
				t.Fatalf("got %d methods, want %d", got, want)
			}
			if got, want := err == nil, tc.errorIsNil; got != want {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestReflectionClient_ResolveService(t *testing.T) {
	cases := []struct {
		name        string
		serviceName string
		descIsNil   bool
		error       *perrors.ProxyError
	}{
		{
			name:        "found",
			serviceName: test.TestService,
			descIsNil:   false,
			error:       nil,
		},
		{
			name:        "not found",
			serviceName: test.NotFoundService,
			descIsNil:   true,
			error: &perrors.ProxyError{
				Code:    perrors.ServiceNotFound,
				Message: fmt.Sprintf("service %s was not found upstream", "not.found.NoService"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newReflectionClient(&test.MockGrpcreflectClient{FileDescriptor: test.NewFileDescriptor(t, test.File)})
			serviceDesc, err := c.resolveService(tc.serviceName)
			if got, want := serviceDesc == nil, tc.descIsNil; got != want {
				t.Fatalf("got %t, want %t", got, want)
			}
			{
				err, ok := err.(*perrors.ProxyError)
				if !ok {
					err = nil
				}
				if got, want := err, tc.error; !reflect.DeepEqual(got, want) {
					t.Fatalf("got %v, want %v", got, want)
				}
			}
		})
	}
}

func TestServiceDescriptor_GetMethods(t *testing.T) {
	fd := test.NewFileDescriptor(t, test.File)
	serviceDesc := ServiceDescriptorFromFileDescriptor(fd, test.TestService)
	if serviceDesc == nil {
		t.Fatalf("service descriptor is nil")
	}
	mds, err := serviceDesc.GetMethods()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"EmptyCall",
		"UnaryCall",
		"StreamingOutputCall",
		"StreamingInputCall",
		"FullDuplexCall",
		"HalfDuplexCall",
	}
	got := make([]string, len(mds))
	for i, m := range mds {
		got[i] = m.GetName()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestServiceDescriptor_FindMethodByName(t *testing.T) {
	cases := []struct {
		name       string
		methodName string
		descIsNil  bool
		error      *perrors.ProxyError
	}{
		{
			name:       "method found",
			methodName: test.EmptyCall,
			descIsNil:  false,
			error:      nil,
		},
		{
			name:       "method not found",
			methodName: test.NotFoundCall,
			descIsNil:  true,
			error: &perrors.ProxyError{
				Code:    perrors.MethodNotFound,
				Message: fmt.Sprintf("the method %s was not found", test.NotFoundCall),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := test.NewFileDescriptor(t, test.File)
			serviceDesc := ServiceDescriptorFromFileDescriptor(file, test.TestService)
			if serviceDesc == nil {
				t.Fatalf("service descriptor is nil")
			}
			methodDesc, err := serviceDesc.FindMethodByName(tc.methodName)
			if got, want := methodDesc == nil, tc.descIsNil; got != want {
				t.Fatalf("got %t, want %t", got, want)
			}
			{
				err, ok := err.(*perrors.ProxyError)
				if !ok {
					err = nil
				}
				if got, want := err, tc.error; !reflect.DeepEqual(got, want) {
					t.Fatalf("got %v, want %v", got, want)
				}
			}
		})
	}
}

func TestMethodDescriptor_GetInputType(t *testing.T) {
	cases := []struct {
		name        string
		serviceName string
		methodName  string
		descIsNil   bool
	}{
		{
			name:        "input type found",
			serviceName: test.TestService,
			methodName:  test.UnaryCall,
			descIsNil:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := test.NewFileDescriptor(t, test.File)
			serviceDesc := ServiceDescriptorFromFileDescriptor(file, tc.serviceName)
			methodDesc, err := serviceDesc.FindMethodByName(tc.methodName)
			if err != nil {
				t.Fatalf(err.Error())
			}
			inputMsgDesc := methodDesc.GetInputType()
			if got, want := inputMsgDesc == nil, tc.descIsNil; got != want {
				t.Fatalf("got %t, want %t", got, want)
			}
		})
	}
}

func TestMethodDescriptor_GetOutputType(t *testing.T) {
	cases := []struct {
		name        string
		serviceName string
		methodName  string
		descIsNil   bool
	}{
		{
			name:        "output type found",
			serviceName: test.TestService,
			methodName:  test.EmptyCall,
			descIsNil:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := test.NewFileDescriptor(t, test.File)
			serviceDesc := ServiceDescriptorFromFileDescriptor(file, tc.serviceName)
			methodDesc, err := serviceDesc.FindMethodByName(tc.methodName)
			if err != nil {
				t.Fatalf(err.Error())
			}
			inputMsgDesc := methodDesc.GetOutputType()
			if got, want := inputMsgDesc == nil, tc.descIsNil; got != want {
				t.Fatalf("got %t, want %t", got, want)
			}
		})
	}
}

func TestMethodDescriptor_GetName(t *testing.T) {
	cases := []struct {
		name        string
		serviceName string
		methodName  string
		descIsNil   bool
	}{
		{
			name:        "get method name",
			serviceName: test.TestService,
			methodName:  test.EmptyCall,
			descIsNil:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := test.NewFileDescriptor(t, test.File)
			serviceDesc := ServiceDescriptorFromFileDescriptor(file, tc.serviceName)
			methodDesc, err := serviceDesc.FindMethodByName(tc.methodName)
			if err != nil {
				t.Fatalf(err.Error())
			}

			if got, want := methodDesc.GetName(), tc.methodName; got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
}

func TestMessageDescriptor_NewMessage(t *testing.T) {
	file := test.NewFileDescriptor(t, test.File)
	serviceDesc := ServiceDescriptorFromFileDescriptor(file, test.TestService)
	if serviceDesc == nil {
		t.Fatal("service descriptor is nil")
	}
	methodDesc, err := serviceDesc.FindMethodByName(test.EmptyCall)
	if err != nil {
		t.Fatalf(err.Error())
	}
	inputMsgDesc := methodDesc.GetInputType()
	inputMsg := inputMsgDesc.NewMessage()
	if got, want := inputMsg == nil, false; got != want {
		t.Fatalf("got %t, want %t", got, want)
	}
}

func TestMessageDescriptor_GetFullyQualifiedName(t *testing.T) {
	file := test.NewFileDescriptor(t, test.File)
	serviceDesc := ServiceDescriptorFromFileDescriptor(file, test.TestService)
	if serviceDesc == nil {
		t.Fatal("service descriptor is nil")
	}
	methodDesc, err := serviceDesc.FindMethodByName(test.UnaryCall)
	if err != nil {
		t.Fatalf(err.Error())
	}
	inputMsgDesc := methodDesc.GetInputType()
	name := inputMsgDesc.GetFullyQualifiedName()
	if got, want := name, test.UnaryCallInputMsgName; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestMessage_MarshalJSON(t *testing.T) {
	file := test.NewFileDescriptor(t, test.File)
	cases := []struct {
		name string
		json []byte
		error
	}{
		{
			name:  "success",
			json:  []byte("{\"body\":\"aGVsbG8=\"}"),
			error: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			messageDesc := file.FindMessage(test.MessageName)
			if messageDesc == nil {
				t.Fatal("messageImpl descriptor is nil")
			}
			message := messageImpl{
				Message: dynamic.NewMessage(messageDesc),
			}
			message.Message.SetField(message.Message.FindFieldDescriptorByName("body"), []byte("hello"))
			j, err := message.MarshalJSON()
			if got, want := j, tc.json; !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			if got, want := err, tc.error; !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestMessage_UnmarshalJSON(t *testing.T) {
	file := test.NewFileDescriptor(t, test.File)
	cases := []struct {
		name string
		json []byte
		error
	}{
		{
			name:  "success",
			json:  []byte("{\"body\":\"aGVsbG8=\"}"),
			error: nil,
		},
		{
			name: "type mismatch",
			json: []byte("{\"body\":\"hello!\""),
			error: &perrors.ProxyError{
				Code:    perrors.MessageTypeMismatch,
				Message: "input JSON does not match messageImpl type",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			messageDesc := file.FindMessage(test.MessageName)
			if messageDesc == nil {
				t.Fatal("messageImpl descriptor is nil")
			}
			message := messageImpl{
				Message: dynamic.NewMessage(messageDesc),
			}
			err := message.UnmarshalJSON(tc.json)

			expectedMessage := dynamic.NewMessage(messageDesc)
			expectedMessage.SetField(expectedMessage.FindFieldDescriptorByName("body"), []byte("hello!"))

			if got, want := err, tc.error; !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}
