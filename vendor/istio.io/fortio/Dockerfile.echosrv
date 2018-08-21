# Build the binaries in larger image
FROM istio/fortio.build:v8 as build
WORKDIR /go/src/istio.io
COPY . fortio
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-s' -o echosrv.bin istio.io/fortio/echosrv
# Minimal image with just the binary
FROM scratch
COPY --from=build /go/src/istio.io/echosrv.bin /usr/local/bin/echosrv
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/echosrv"]
