# Getting the source tree ready for running tests (sut)
FROM docker.io/fortio/fortio.build:v12
WORKDIR /go/src/fortio.org
COPY . fortio
RUN make -C fortio dependencies
