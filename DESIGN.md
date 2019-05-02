# Design Goals

grpc-mate is aimed to be a gRPC Service sidecar that serving HTTP requests, which can then be reverse-proxied to the gRPC Service, just like [grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway). It is different than grpc-gateway in that it dynamically maps the gRPC service definitions into HTTP endpoints, without having to compile against the Proto definition files and generate code upon service definition updates, like grpc-gateway does.

The design goal is to make grpc-mate a generic gRPC service sidecar that effectively translate a gRPC service to HTTP service, yet keep as lightweight as possible.

The following features describe above design goals in detail.

* It should be a generic middleware for translating gRPC service to HTTP. It should not need to re-generate code or re-build the proxy itself in order to work with different gRPC service, only configuration should be applied.
* It should require minimal impact on the proxied gRPC proto definition or service implementation. For example, protobuf options might be applied to further describe how to map gRPC service into HTTP endpoints, but there should be default mapping rules so that it does not need any addtional options (like grpc-gateway does). However, gRPC service reflection is required to be registered along with the service implementation. This should be the only planned requirement for the service to work with grpc-mate.
* It should not depend on kubernetes, or other service registar infrastructure. However, it should be very easy to run within kubernetes, which means no intrusion to the original service/deployment description. Ideally, it should be merely some addtional metadata to declare.

Here is a couple of explanations on why certain design decisions are made.

1. Why does it run as sidecar alongside the gRPC service instance, instead of sitting in front of all gRPC services and instances?

The latter might look familar to a tradtional gateway, and might require less resource. However, it might be quite complicate to loadbalance to all service instances. It needs to hook in to the service registration facility in order to know which backend gRPC service to proxy to. This introduces complexity that grpc-mate, as a simple and lightweight proxy, wants to avoid. Besides, proxy to multiple different services, means the proxy needs to by dynamic enough, so that backend gRPC services upgrading does not require the proxy to be re-configured and restarted. This also adds extra complexity. Running as sidecar, this won't be problem as the sidecar can be restarted and reconfigured alongside the gRPC service instance is re-deployed. This design choice is also the main difference between grpc-mate and [grpc-http-proxy](https://github.com/mercari/grpc-http-proxy).

2. Why does it need to enable reflection on proxied gRPC service?

The reflection service enables grpc-mate to inspect the proxied gRPC service definition in runtime, without requirement of having to depend on service protobuf file of the underlying gRPC service.