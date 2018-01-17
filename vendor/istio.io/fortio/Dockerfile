# Build the binaries in larger image
FROM fortio/fortio.build:v4 as build
WORKDIR /go/src/istio.io
COPY . fortio
# NOTE: changes to this file should be propagated to release/Dockerfile.in too
# (wtb docker include)
# Demonstrate moving the static directory outside of the go source tree and
# the default data directory to a /var/lib/... volume
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags \
  '-X istio.io/fortio/ui.resourcesDir=/usr/local/lib/fortio -X main.defaultDataDir=/var/lib/istio/fortio -s' \
  -o fortio.bin istio.io/fortio
# Just check it stays compiling on Windows (would need to set the rsrcDir too)
RUN CGO_ENABLED=0 GOOS=windows go build -a -o fortio.exe istio.io/fortio
# Minimal image with just the binary and certs
FROM scratch as release
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /go/src/istio.io/fortio/ui/static /usr/local/lib/fortio/static
COPY --from=build /go/src/istio.io/fortio/ui/templates /usr/local/lib/fortio/templates
COPY --from=build /go/src/istio.io/fortio.bin /usr/local/bin/fortio
EXPOSE 8079
EXPOSE 8080
EXPOSE 8081
VOLUME /var/lib/istio/fortio
ENTRYPOINT ["/usr/local/bin/fortio"]
# start the server mode (grpc ping on 8079, http echo and UI on 8080, redirector on 8081) by default
CMD ["server"]
