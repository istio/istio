FROM golang:latest
WORKDIR /go/src/github.com/envoyproxy/go-control-plane/
ARG test_out_bin=bin/test-linux
ENV CGO_ENABLED 1
ENV GOOS linux
ENV GOARCH amd64
COPY . .
RUN go build -race -o ${test_out_bin} pkg/test/main/main.go

# Integration test docker file
# Due to https://github.com/envoyproxy/envoy/issues/6237, we have to pin envoy temporarily
FROM envoyproxy/envoy:0634e7bcbfea7317eaf805a657d048ffa27f519a
ADD sample /sample
ADD build/integration.sh build/integration.sh
COPY --from=0 /go/src/github.com/envoyproxy/go-control-plane/bin/test-linux /bin/test
ENTRYPOINT ["build/integration.sh"]
