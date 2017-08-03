FROM golang:1.8.3
WORKDIR /go/src/istio.io/istio/devel
COPY . fortio
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-s' istio.io/istio/devel/fortio/cmd/echosrv

FROM scratch
COPY --from=0 /go/src/istio.io/istio/devel/echosrv /usr/local/bin/echosrv
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/echosrv"]
