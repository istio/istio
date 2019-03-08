FROM ubuntu:xenial

RUN apt-get -y update && apt-get --no-install-recommends -y install ca-certificates
ADD statsdadapter /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/statsdadapter"]
