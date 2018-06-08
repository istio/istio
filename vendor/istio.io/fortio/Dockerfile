# Build the binaries in larger image
FROM istio/fortio.build:v7 as build
WORKDIR /go/src/istio.io
COPY . fortio
# Submodule handling
RUN make -C fortio submodule
# Putting spaces in linker replaced variables is hard but does work.
RUN echo "$(date +'%Y-%m-%d %H:%M') $(cd fortio; git rev-parse HEAD)" > /build-info.txt
# Sets up the static directory outside of the go source tree and
# the default data directory to a /var/lib/... volume
# + rest of build time/git/version magic.
RUN echo "-s -X istio.io/fortio/ui.resourcesDir=/usr/local/lib/fortio -X main.defaultDataDir=/var/lib/istio/fortio \
  -X \"istio.io/fortio/version.buildInfo=$(cat /build-info.txt)\" \
  -X istio.io/fortio/version.tag=$(cd fortio; git describe --tags --match 'v*') \
  -X istio.io/fortio/version.gitstatus=$(cd fortio; git status --porcelain | wc -l)" | tee /link-flags.txt
RUN go version
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags "$(cat /link-flags.txt)" -o fortio_go1.10.bin istio.io/fortio
RUN ./fortio_go1.10.bin version
# Check we still build with go 1.8 (and macos does not break)
RUN /usr/local/go/bin/go version
RUN CGO_ENABLED=0 GOOS=darwin /usr/local/go/bin/go build -a -ldflags "$(cat /link-flags.txt)" -o fortio_go1.8.mac istio.io/fortio
# Build with 1.8 for perf comparison
#RUN CGO_ENABLED=0 GOOS=linux /usr/local/go/bin/go build -a -ldflags "$(cat /link-flags.txt)" -o fortio_go1.8.bin istio.io/fortio
#RUN ./fortio_go1.8.bin version
# Just check it stays compiling on Windows (would need to set the rsrcDir too)
RUN CGO_ENABLED=0 GOOS=windows go build -a -o fortio.exe istio.io/fortio
#RUN ln -s /usr/local/bin/fortio_go1.8 /usr/local/bin/fortio
#RUN tar cvf /tmp/symlink.tar /usr/local/bin/fortio
# Minimal image with just the binary and certs
FROM scratch as release
# NOTE: the list of files here, if updated, must be changed in release/Dockerfile.in too
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /go/src/istio.io/fortio/ui/static /usr/local/lib/fortio/static
COPY --from=build /go/src/istio.io/fortio/ui/templates /usr/local/lib/fortio/templates
#COPY --from=build /go/src/istio.io/fortio_go1.10.bin /usr/local/bin/fortio_go1.10
#COPY --from=build /go/src/istio.io/fortio_go1.8.bin /usr/local/bin/fortio_go1.8
COPY --from=build /go/src/istio.io/fortio_go1.10.bin /usr/local/bin/fortio
EXPOSE 8079
EXPOSE 8080
EXPOSE 8081
VOLUME /var/lib/istio/fortio
ENTRYPOINT ["/usr/local/bin/fortio"]
# start the server mode (grpc ping on 8079, http echo and UI on 8080, redirector on 8081) by default
CMD ["server"]
