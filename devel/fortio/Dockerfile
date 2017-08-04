# Build the binaries in larger image
FROM golang:1.8.3 as build
WORKDIR /go/src/istio.io/istio/devel
RUN go get google.golang.org/grpc
COPY . fortio
RUN \
  CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-s' -o echosrv.bin istio.io/istio/devel/fortio/cmd/echosrv && \
  CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-s' -o grpcping.bin istio.io/istio/devel/fortio/cmd/grpcping && \
  CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-s' -o fortio.bin istio.io/istio/devel/fortio/cmd/fortio
# Minimal image with just the binaries and certs
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /go/src/istio.io/istio/devel/echosrv.bin /usr/local/bin/echosrv
COPY --from=build /go/src/istio.io/istio/devel/grpcping.bin /usr/local/bin/grpcping
COPY --from=build /go/src/istio.io/istio/devel/fortio.bin /usr/local/bin/fortio
EXPOSE 8079
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/echosrv"]
