FROM ubuntu:xenial

RUN apt-get -y update && apt-get --no-install-recommends -y install ca-certificates
ADD memquotaadapter /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/memquotaadapter"]
