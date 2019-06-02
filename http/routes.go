package http

func (s *Server) registerHandlers(grpcClient GrpcClient) {
	s.router.HandleFunc("/actuator/health", s.HealthCheckHandler())
	s.router.HandleFunc("/actuator/services", s.IntrospectHandler(grpcClient))
	s.router.HandleFunc("/v1/", apply(s.RPCCallHandler(grpcClient), []Adapter{s.withLog}...))
	s.router.HandleFunc("/", apply(s.CatchAllHandler(), []Adapter{s.withLog}...))
}
