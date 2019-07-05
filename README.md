# gRPC Mate

[![Go Report Card](https://goreportcard.com/badge/github.com/gdong42/grpc-mate)](https://goreportcard.com/report/github.com/gdong42/grpc-mate)
[![Build Status](https://travis-ci.com/gdong42/grpc-mate.svg?branch=master)](https://travis-ci.com/gdong42/grpc-mate)
[![Docker Pulls](https://img.shields.io/docker/pulls/gdong/grpc-mate.svg)](https://hub.docker.com/r/gdong/grpc-mate)
[![MicroBadger Size (tag)](https://img.shields.io/microbadger/image-size/gdong/grpc-mate/latest.svg)](https://hub.docker.com/r/gdong/grpc-mate)
[![Docker Image](https://images.microbadger.com/badges/version/gdong/grpc-mate.svg)](https://hub.docker.com/r/gdong/grpc-mate)

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

It's recommended to use pre-built docker image `gdong/grpc-mate` directly. You can also choose to build from source.

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

## Quick Start

### Prerequisites

Make sure [Server Reflection](https://github.com/grpc/grpc/blob/master/doc/server-reflection.md) is enabled on gRPC target server side.
* For server written in Java, refer to [this guide](https://github.com/grpc/grpc-java/blob/master/documentation/server-reflection-tutorial.md)
    ```diff
    --- a/examples/build.gradle
    +++ b/examples/build.gradle
    @@ -27,6 +27,7 @@
    dependencies {
    compile "io.grpc:grpc-netty-shaded:${grpcVersion}"
    compile "io.grpc:grpc-protobuf:${grpcVersion}"
    +  compile "io.grpc:grpc-services:${grpcVersion}"
    compile "io.grpc:grpc-stub:${grpcVersion}"
    
    testCompile "junit:junit:4.12"
    --- a/examples/src/main/java/io/grpc/examples/helloworld/HelloWorldServer.java
    +++ b/examples/src/main/java/io/grpc/examples/helloworld/HelloWorldServer.java
    @@ -33,6 +33,7 @@ package io.grpc.examples.helloworld;
    
    import io.grpc.Server;
    import io.grpc.ServerBuilder;
    +import io.grpc.protobuf.services.ProtoReflectionService;
    import io.grpc.stub.StreamObserver;
    import java.io.IOException;
    import java.util.logging.Logger;
    @@ -50,6 +51,7 @@ public class HelloWorldServer {
        int port = 50051;
        server = ServerBuilder.forPort(port)
            .addService(new GreeterImpl())
    +        .addService(ProtoReflectionService.newInstance())
            .build()
            .start();
        logger.info("Server started, listening on " + port);
    ```
* For server written in Go, refer to [this guide](https://github.com/grpc/grpc-go/blob/master/Documentation/server-reflection-tutorial.md)
    ```diff
    --- a/examples/helloworld/greeter_server/main.go
    +++ b/examples/helloworld/greeter_server/main.go
    @@ -40,6 +40,7 @@ import (
            "google.golang.org/grpc"
            pb "google.golang.org/grpc/examples/helloworld/helloworld"
    +       "google.golang.org/grpc/reflection"
    )

    const (
    @@ -61,6 +62,8 @@ func main() {
            }
            s := grpc.NewServer()
            pb.RegisterGreeterServer(s, &server{})
    +       // Register reflection service on gRPC server.
    +       reflection.Register(s)
            if err := s.Serve(lis); err != nil {
                    log.Fatalf("failed to serve: %v", err)
            }
    ```

For demonstration, we start the gRPC example server with reflection enabled provided at https://github.com/grpc/grpc-go/tree/master/examples/features/reflection

```
$ go run server/main.go
server listening at [::]:50051
```

### Run gRPC Mate

It's really simple to run. Let's connect to the gRPC server started above as an example, using docker or command built from source.

#### Run gRPC Mate via Docker

```
$ docker run --name grpc-mate -e GRPC_MATE_PROXIED_HOST=<your grpc server local IP> -e GRPC_MATE_PROXIED_PORT=50051 -dp 6600:6600 gdong/grpc-mate
```

Note above `GRPC_MATE_PROXIED_HOST` has to be set to your IP address other than localhost, so that grpc-mate running inside docker can access it.

#### Run gRPC Mate directly
```
$ GRPC_MATE_PROXIED_PORT=50051 ./grpc-mate
```
This by default listens on 6600 as HTTP port, and connects to a local gRPC server running at `localhost:50051`

To connect to other gRPC server host and port, refer to the configuration section.

### Introspecting Services

Now try get `http://localhost:6600/actuator/services`, you will see all services the server exposes, as well as their enclosing methods, input and output types, e.g. one element of `services`:
```
      {  
         "name":"helloworld.Greeter",
         "methods":[  
            {  
               "name":"SayHello",
               "input":"helloworld.HelloRequest",
               "output":"helloworld.HelloReply",
               "route":"/helloworld.Greeter/SayHello"
            }
         ]
      }
```
 It also has request/response JSON templates, convenient for construcing HTTP and JSON requests, e.g. one element of `types`:

```
      {  
         "name":"helloworld.HelloRequest",
         "template":{  
            "name":""
         }
      }

```

### Making Requests

Now let's try making gRPC requests using above inspected information

```
$ curl -X POST -d '{"name":"gdong42"}' "http://localhost:6600/v1/helloworld.Greeter/SayHello" 
{"message":"Hello gdong42"}
```
Above we invoked `SayHello` method of `helloworld.Greeter` service, with JSON message of `helloworld.HelloRequest` type, and got a JSON message of `helloworld.HelloReply` type.

Note the HTTP method is POST, the body is a JSON string, and the request path is of pattern `/v1/{serviceName}/{methodName}`.

## Configuration

gRPC Mate is configured via a group of `GRPC_MATE_` prefixed Environment variables. They are

* `GRPC_MATE_PORT`: the HTTP Port grpc-mate listens on, defaults to 6600
* `GRPC_MATE_PROXIED_HOST`: the backend gRPC Host grpc-mate connects to, defaults to 127.0.0.1
* `GRPC_MATE_PROXIED_PORT`: the backend gRPC Port grpc-mate connects to, defaults to 9090
* `GRPC_MATE_LOG_LEVEL`: the log level, must be INFO, DEBUG, or ERROR, defaults to INFO

## Limitation

Currently, gRPC Mate works with Unary calls only. We are working on support Streaming as well.

## Contributing

All kinds of contribution are welcome!

## Credits
* [mercari/grpc-http-proxy](https://github.com/mercari/grpc-http-proxy) - gRPC Mate project is originally forked from this project. Although going towards different directions in [design decisions](https://github.com/gdong42/grpc-mate/blob/master/DESIGN.md), many coding implementations are borrowed from it.
* [jhump/protoreflect](https://github.com/jhump/protoreflect) - The main lowlevel building block, which does the heavy lifting.
* [fullstorydev/grpcurl](https://github.com/fullstorydev/grpcurl) - A useful command line tool to interact with gRPC service, gRPC Mate is also like the HTTP interface of this cli tool. A good example of protoreflect usage mentioned above.

## License

Apache License 2.0
