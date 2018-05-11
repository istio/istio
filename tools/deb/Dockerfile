# Test installing of the deb file
FROM istionightly/base_debug

# Micro pilot+mock mixer+echo
ADD hyperistio /usr/local/bin/hyperistio
ADD *.yaml /var/lib/istio/config/
ADD certs/* /var/lib/istio/certs/

ADD istio.deb /tmp
RUN dpkg -i /tmp/istio.deb && rm /tmp/istio.deb

