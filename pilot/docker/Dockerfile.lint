FROM    golang:1.8.3-alpine

RUN     apk add -U git

ARG     GOMETALINTER_SHA=bfcc1d6942136fd86eb6f1a6fb328de8398fbd80
RUN     go get -d github.com/alecthomas/gometalinter && \
        cd /go/src/github.com/alecthomas/gometalinter && \
        git checkout -q "$GOMETALINTER_SHA" && \
        go build -v -o /usr/local/bin/gometalinter . && \ 
        gometalinter --install && \
        rm -rf /go/src/* /go/pkg/*

ENV     CGO_ENABLED=0

ENTRYPOINT ["/usr/local/bin/gometalinter"]
