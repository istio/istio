FROM istionightly/base_debug

ADD pkg-test-echo-cmd-client /usr/local/bin/client
ADD pkg-test-echo-cmd-server /usr/local/bin/server
ADD certs/cert.crt /cert.crt
ADD certs/cert.key /cert.key
ENTRYPOINT ["/usr/local/bin/server"]
