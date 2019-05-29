# gRPC Mate

gRPC Mate is a light weight reverse proxy server that translates JSON HTTP requests into gRPC calls without the need of 
code generation. It reads protobuf service definitions through accessing reflection API exposed by the gRPC service, 
and converts HTTP and JSON requests to gRPC invocations dynamically.

The purpose that gRPC Mate is created is to provide a way to serve HTTP and JSON from a gRPC server very easily, without 
protobuf definition files sharing, without protobuf directives, and without any Service Discovery system integration. 

As its name *gRPC Mate* suggestes, the reverse proxy server is designed to be a sidecar, and is expected to run beside 
the proxied gRPC server. In a development or testing scenario, it can be dropped with mere configuration in front of the
target gRPC service. In a production environment, instead of running a single gRPC Mate server in front all gRPC services,
it is better to be deployed alongside each gRPC server instances, for example, as another container in the same pod if 
running within Kubernetes cluster.

## Features

* **HTTP to gRPC Translation** - Translates gRPC services into HTTP JSON endpoints.
* **Independent Reverse Proxy** - Runs as an independent proxy server against upstream gRPC service, and evolving of the underlying gRPC service does not require an upgrade or rebuild
of gRPC Mate server.
* **Easy to Setup** - Requires only 1) the upstream gRPC server enabling gRPC reflection service, and 2) the gRPC listening address and port passing to gRPC Mate.
* **Management Endpoints** - Provides basic management endpoints, such as `/actuator/health` 
telling if the service is healthy, and `/actuator/services` introspecting all services, methods, their HTTP route mappings and request/response schema example.

## Installation

### Build from source

1. Clone this repo
    ```
    git clone git@github.com:gdong42/grpc-mate.git
    ```
2. Install Go SDK if it's not installed yet, e.g.
   ```
   brew install go
   brew install dep
   ```
3. Setup `GOPATH` if not yet, go to grpc-mate dir, install dependencies and build binary
    ```
    dep ensure
    go build -o grpc-mate
    ```
Now grpc-mate command is built, following sections show how you configure and run it.

## Usage

It's really simple to run:
```
./grpc-mate
```
This by default listens on 6666 as HTTP port, and connects to a local gRPC server running at `localhost:9090`

To connect to other gRPC server host and port, refer to more Environment settings in next section

## Configuration

## Contribution

## Credits

## License





