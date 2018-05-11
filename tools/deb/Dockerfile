# Base dockerfile containing ubuntu and istio debian.
# Can be used for testing
FROM istionightly/base_debug

# Micro pilot+mock mixer+echo
COPY hyperistio /usr/local/bin/hyperistio
COPY *.yaml /var/lib/istio/config/
COPY certs/ /var/lib/istio/

COPY istio.deb /tmp
COPY istio-sidecar.deb /tmp
COPY deb_test.sh /usr/local/bin/

RUN dpkg -i /tmp/istio-sidecar.deb && rm /tmp/istio-sidecar.deb
RUN dpkg -i /tmp/istio.deb && rm /tmp/istio.deb

