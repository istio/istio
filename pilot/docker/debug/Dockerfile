FROM golang:latest AS getter
RUN go get -u -v github.com/go-delve/delve/cmd/dlv
#COPY dlv    .

FROM docker.io/therealmitchconnors/pilot:therealmitchconnors

EXPOSE 40000
COPY --from=0 /go/bin/dlv .
ENTRYPOINT ["./dlv", "--listen=:40000", "--headless=true", "--api-version=2", "exec", "/usr/local/bin/pilot-discovery", "--"]
