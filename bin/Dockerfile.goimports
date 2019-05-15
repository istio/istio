FROM golang:1.11.2-alpine3.8 as build
RUN apk add --update --no-cache git bash    && \
    go get golang.org/x/tools/cmd/goimports && \
    cd /go/src/golang.org/x/tools           && \
    git checkout 379209517ffe               && \
    go build -o /goimports ./cmd/goimports

FROM alpine
ENV GOPATH=/go
COPY --from=build /goimports /goimports
