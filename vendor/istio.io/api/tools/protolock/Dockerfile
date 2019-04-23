FROM alpine:3.7 as downloader
RUN apk update && \
        apk add curl && \
        curl -L https://github.com/nilslice/protolock/releases/download/v0.10.1/protolock.20190211T154117Z.linux-amd64.tgz \ 
        -o protolock.tgz && \
        tar -zxvf protolock.tgz && \
        chmod +x protolock && \
        mkdir -p /out/usr/bin && \
        cp protolock /out/usr/bin/protolock

FROM alpine:3.7
COPY --from=downloader /out/ /
ENTRYPOINT ["/usr/bin/protolock"]
