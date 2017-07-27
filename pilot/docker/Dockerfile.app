FROM scratch
ADD client /usr/local/bin/client
ADD server /usr/local/bin/server
ENTRYPOINT ["/usr/local/bin/server"]
