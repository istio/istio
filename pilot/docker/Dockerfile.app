FROM istionightly/base_debug

ADD pilot-test-client /usr/local/bin/client
ADD pilot-test-server /usr/local/bin/server
ADD cert.crt /cert.crt
ADD cert.key /cert.key
ENTRYPOINT ["/usr/local/bin/server"]
