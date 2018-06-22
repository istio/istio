# Getting the source tree ready for running tests (sut)
FROM istio/fortio.build:v8
WORKDIR /go/src/istio.io
COPY . fortio
RUN make -C fortio dependencies
