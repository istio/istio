FROM istionightly/base_debug

ADD pkg-test-application-echo-client /usr/local/bin/client
ADD pkg-test-application-echo-server /usr/local/bin/server
ADD cert.crt /cert.crt
ADD cert.key /cert.key
ENTRYPOINT ["/usr/local/bin/server"]
