# Apache SkyWalking Istio Adaptor
Apache SkyWalking 6+ accepts telemetry data from Istio, by providing an Istio adaptor. 

See [official document](https://github.com/apache/incubator-skywalking/blob/master/docs/README.md) to set the probe. 

## Compile and Test
The following commands help the developer of SkyWalking Istio probe developers to test codes.

1. Compile and Startup Mixer server
> pushd $ISTIO/istio && make mixs
> $GOPATH/out/darwin_amd64/release/mixs server --configStoreURL=fs://$(pwd)/mixer/adapter/skywalking/testdata

2. Compile and Startup Mixer simulator client
> pushd $ISTIO/istio && make mixc
> $GOPATH/out/darwin_amd64/release/mixc report -s destination.workload.name="svc.cluster.local",source.workload.name="serviceA",source.uid="serviceA-inst1",destination.service="serviceB",destination.uid="serviceB-int2",api.protocol="http",request.method="get",request.path="/test/api",context.reporter.kind="inbound" -t request.time="2017-02-01T10:12:14Z",response.time="2017-02-01T10:12:19Z" -i response.code="200"

3. Generate grpc codes of SkyWalking protocol
service-mesh.proto could be found at [Apache SkyWalking service mesh probe protocol](https://github.com/apache/incubator-skywalking-data-collect-protocol/tree/master/service-mesh-probe).
> protoc --go_out=plugins=grpc:. service-mesh.proto 
  