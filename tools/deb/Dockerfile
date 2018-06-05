# Base dockerfile containing ubuntu and istio debian.
# Can be used for testing
FROM istionightly/base_debug

# Micro pilot+mock mixer+echo, local kube
COPY hyperistio kube-apiserver etcd kubectl /usr/local/bin/
COPY *.yaml /var/lib/istio/config/
COPY certs/ /var/lib/istio/
COPY certs/default/* /etc/certs/

COPY istio.deb /tmp
COPY istio-sidecar.deb /tmp
COPY deb_test.sh /usr/local/bin/

# Root and istio are not intercepted
RUN adduser istio-test --system

# Verify the debian files can be installed
RUN dpkg -i /tmp/istio-sidecar.deb && rm /tmp/istio-sidecar.deb
RUN dpkg -i /tmp/istio.deb && rm /tmp/istio.deb

