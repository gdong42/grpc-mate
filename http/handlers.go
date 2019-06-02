package http

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	perrors "github.com/gdong42/grpc-mate/errors"
	"github.com/gdong42/grpc-mate/metadata"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	grpc_metadata "google.golang.org/grpc/metadata"
)

type callee struct {
	Service string `json:"service"`
	Method  string `json:"method"`
}

// HealthCheckHandler returns a status code 200 response for liveness probes
func (s *Server) HealthCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{\"status\":\"UP\"}"))
	}
}

// IntrospectHandler handles requests that introspects all services and types
func (s *Server) IntrospectHandler(client GrpcClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// example path and query parameter:
		// example.com/actuator/services - list all services

		if !client.IsReady() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		response, err := client.Introspect()
		if err != nil {
			returnError(w, errors.Cause(err).(perrors.Error))
			s.logger.Error("error in introspection",
				zap.String("err", err.Error()))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(response)
		return
	}
}

// CatchAllHandler handles requests for non-existing paths
// This is done explicitly in order to have the logger middleware log the fact
func (s *Server) CatchAllHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}
}

// RPCCallHandler handles requests for making gRPC calls
func (s *Server) RPCCallHandler(client GrpcClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// TODO supports method directives in pb?
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// example path and query parameter:
		// example.com/v1/svc/method?version=v1
		parts := strings.Split(r.URL.Path, "/")
		s.logger.Info(fmt.Sprintf("%v", parts))
		if len(parts) != 4 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		service := parts[2]
		method := parts[3]
		if service == "" || method == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !client.IsReady() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		c := callee{
			Service: service,
			Method:  method,
		}
		ctx := grpc_metadata.NewOutgoingContext(r.Context(),
			grpc_metadata.MD(metadata.MetadataFromHeaders(r.Header)))

		md := make(metadata.Metadata)

		inputMessage, err := ioutil.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		response, err := client.Invoke(ctx, c.Service, c.Method, inputMessage, &md)
		if err != nil {
			returnError(w, errors.Cause(err).(perrors.Error))
			s.logger.Error("error in handling call",
				zap.String("err", err.Error()))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(response)
		return
	}
}

func returnError(w http.ResponseWriter, err perrors.Error) {
	w.WriteHeader(err.HTTPStatusCode())
	err.WriteJSON(w)
	return
}
