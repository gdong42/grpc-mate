package proxy

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/fullstorydev/grpcurl"
	"github.com/gdong42/grpc-mate/proxy/reflection"
	"github.com/gdong42/grpc-mate/proxy/test"
)

type introspectionReflector struct {
	reflection.Reflector
	methods []*reflection.MethodDescriptor
}

func (r *introspectionReflector) ListServices() ([]string, error) {
	return []string{"example.First", "example.Second", "example.Empty"}, nil
}

func (r *introspectionReflector) DescribeService(name string) ([]*reflection.MethodDescriptor, error) {
	if name == "example.Empty" {
		return nil, nil
	}
	return r.methods, nil
}

func TestIntrospectionFilters(t *testing.T) {
	fd := test.NewFileDescriptor(t, test.File)
	r := reflection.NewReflector(&test.MockGrpcreflectClient{FileDescriptor: fd})
	methods, err := r.DescribeService(test.TestService)
	if err != nil {
		t.Fatal(err)
	}
	source, err := grpcurl.DescriptorSourceFromFileDescriptors(fd)
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{reflector: &introspectionReflector{methods: methods}, descSource: source}
	allJSON, err := p.introspect("", "")
	if err != nil {
		t.Fatal(err)
	}
	var all IntrospectionResponse
	if err := json.Unmarshal(allJSON, &all); err != nil {
		t.Fatal(err)
	}
	if len(all.Services) != 3 || len(all.Types) != 7 || len(all.Services[0].Methods) != 6 || len(all.Services[1].Methods) != 6 || len(all.Services[2].Methods) != 0 {
		t.Fatalf("unfiltered response lost services, methods or types: %s", allJSON)
	}
	allTypes := make(map[string]*typeElement)
	for _, typ := range all.Types {
		allTypes[typ.Name] = typ
	}
	for _, tc := range []struct {
		label, name, method      string
		services, methods, types int
	}{
		{"no query", "", "", 3, len(methods) * 2, len(allTypes)},
		{"service", "example.First", "", 1, len(methods), len(allTypes)},
		{"method across services", "", "UnaryCall", 2, 2, 2},
		{"both", "example.Second", "UnaryCall", 1, 1, 2},
		{"same input and output", "", "EmptyCall", 2, 2, 1},
		{"shared types across methods", "example.First", "", 1, len(methods), len(allTypes)},
		{"unknown service", "missing", "", 0, 0, 0},
		{"unknown method", "", "missing", 0, 0, 0},
		{"combined no match", "example.Empty", "UnaryCall", 0, 0, 0},
		{"empty service", "example.Empty", "", 1, 0, 0},
		{"service exact", "example", "", 0, 0, 0},
		{"method exact", "", "Unary", 0, 0, 0},
		{"case sensitive", "", "unarycall", 0, 0, 0},
	} {
		t.Run(tc.label, func(t *testing.T) {
			js, err := p.introspect(tc.name, tc.method)
			if err != nil {
				t.Fatal(err)
			}
			var got IntrospectionResponse
			if err := json.Unmarshal(js, &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Services) != tc.services || len(got.Types) != tc.types {
				t.Fatalf("services/types=%d/%d, want %d/%d; %s", len(got.Services), len(got.Types), tc.services, tc.types, js)
			}
			needed := make(map[string]bool)
			count := 0
			for _, svc := range got.Services {
				if tc.name != "" && svc.Name != tc.name {
					t.Errorf("unexpected service %s", svc.Name)
				}
				for _, m := range svc.Methods {
					count++
					if tc.method != "" && m.Name != tc.method {
						t.Errorf("unexpected method %s", m.Name)
					}
					if m.Route != "/"+svc.Name+"/"+m.Name {
						t.Errorf("unexpected route %s", m.Route)
					}
					needed[m.InputType], needed[m.OutputType] = true, true
				}
			}
			if count != tc.methods {
				t.Errorf("methods=%d, want %d", count, tc.methods)
			}
			for _, typ := range got.Types {
				if !needed[typ.Name] {
					t.Errorf("unneeded or duplicate type %s", typ.Name)
				}
				delete(needed, typ.Name)
				if !reflect.DeepEqual(typ, allTypes[typ.Name]) {
					t.Errorf("template changed for %s", typ.Name)
				}
			}
			if len(needed) != 0 {
				t.Errorf("missing types: %v", needed)
			}
			if tc.services == 0 && string(js) != `{"services":[],"types":[]}` {
				t.Errorf("unexpected empty response: %s", js)
			}
		})
	}
}
