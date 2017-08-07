FROM scratch
ADD client /usr/local/bin/client
ADD server /usr/local/bin/server
ADD certs/cert.crt /cert.crt
ADD certs/cert.key /cert.key
ENTRYPOINT ["/usr/local/bin/server"]
