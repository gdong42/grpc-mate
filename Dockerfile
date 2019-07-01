FROM golang:1.12-alpine AS builder
RUN apk --no-cache add git bzr mercurial
ENV D=/go/src/github.com/gdong42/grpc-mate
RUN go get -u github.com/golang/dep/...
ADD ./Gopkg.* $D/
RUN cd $D && dep ensure -v --vendor-only
# build
ADD . $D/
RUN cd $D && go build -o grpc-mate && cp grpc-mate /tmp/

FROM alpine
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /tmp/grpc-mate /app
EXPOSE 6600
CMD ["./grpc-mate"]

