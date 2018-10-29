# Build the binaries in larger image
FROM docker.io/fortio/fortio.build:v12 as build
WORKDIR /go/src/fortio.org
COPY . fortio
RUN make -C fortio official-build-version BUILD_DIR=/build OFFICIAL_TARGET=fortio.org/fortio/echosrv OFFICIAL_BIN=../echosrv.bin
# Minimal image with just the binary
FROM scratch
COPY --from=build /go/src/fortio.org/echosrv.bin /usr/bin/echosrv
EXPOSE 8080
ENTRYPOINT ["/usr/bin/echosrv"]
