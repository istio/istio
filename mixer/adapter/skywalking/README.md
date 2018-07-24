## Compile and Startup Mixer server
> pushd $ISTIO/istio && make mixs
> $GOPATH/out/darwin_amd64/release/mixs server --configStoreURL=fs://$(pwd)/mixer/adapter/skywalking/testdata

## Compile and Startup Mixer simulator client
> pushd $ISTIO/istio && make mixc
> $GOPATH/out/darwin_amd64/release/mixc report -s destination.service="svc.cluster.local" -t request.time="2017-02-01T10:12:14Z",response.time="2017-02-01T10:12:19Z" 

## Generate grpc codes
> protoc --go_out=plugins=grpc:. service-mesh.proto 
  