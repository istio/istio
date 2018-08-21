# Build the binaries in larger image
FROM istio/fortio.build:v8 as build
WORKDIR /go/src/istio.io
COPY . fortio
# TODO: share common part with main Dockerfile (have the main Dockerfile build more than 1 image - how?)
RUN echo "$(date +'%Y-%m-%d %H:%M') $(cd fortio; git rev-parse HEAD)" > /build-info.txt
RUN echo "-s -X \"istio.io/fortio/version.buildInfo=$(cat /build-info.txt)\" \
  -X istio.io/fortio/version.tag=$(cd fortio; git describe --tags) \
  -X istio.io/fortio/version.gitstatus=$(cd fortio; git status --porcelain | wc -l)" > /link-flags.txt
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags "$(cat /link-flags.txt)" -o fcurl.bin istio.io/fortio/fcurl
# Minimal image with just the binary
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /go/src/istio.io/fcurl.bin /usr/local/bin/fcurl
ENTRYPOINT ["/usr/local/bin/fcurl"]
