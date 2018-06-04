FROM istionightly/base_debug

ADD pilot-discovery /usr/local/bin/
ADD cacert.pem /cacert.pem
ENTRYPOINT ["/usr/local/bin/pilot-discovery"]
