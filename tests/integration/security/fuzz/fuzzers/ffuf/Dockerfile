FROM ubuntu:noble

#hadolint ignore=DL3009
RUN apt-get update && apt-get install --no-install-recommends -y wget git curl
RUN wget -q https://go.dev/dl/go1.24.4.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf go1.24.4.linux-amd64.tar.gz
ENV PATH=$PATH:/usr/local/go/bin
RUN git clone https://github.com/ffuf/ffuf /ffuf --depth=1
COPY run.sh /ffuf/
WORKDIR /ffuf
RUN go build
CMD [ "sleep", "365d" ]
