# Development Guide

Please see the [Istio development guide](https://github.com/istio/istio/blob/master/devel/README.md) 
for the common Istio-wide development guidelines.

After which here are other Mixer specific docs you should look at:

- [Writing Mixer adapters](./adapters.md)

## Using Mixer locally
The following command runs Mixer locally using local configuration.
The default configuration contain adapters like `stackdriver` that connect to outside systems. If you do not intend to use the  adapter for local testing, you should move `testdata/config/stackdriver.yaml` out of the config directory, otherwise you will see repeated logging of configuration errors.

```shell
KUBECONFIG=${HOME}/.kube/config bazel-bin/cmd/server/mixs server --logtostderr --configStore2URL=fs://$(pwd)/testdata/config --configStoreURL=fs://$(pwd)/testdata/configroot  -v=4
```

You can also run a simple client to interact with the server:

The following command sends a `check` request to Mixer.
```shell
bazel-bin/cmd/client/mixc check  -v 2 --string_attributes destination.service=abc.ns.svc.cluster.local,service.name=myservice   --stringmap_attributes request.headers=clnt2:abc,destination.labels=app:ratings,source.labels=version:v2

Check RPC completed successfully. Check status was OK
  Valid use count: 10000, valid duration: 5m0s
```

The following command sends a `report` request to Mixer.
```shell
bazel-bin/cmd/client/mixc report -v 2 --string_attributes destination.service=abc.ns.svc.cluster.local,service.name=myservice,target.port=8080 --stringmap_attributes "request.headers=clnt:abc;source:abc"  -i response.duration=0.02 -i response.size=1024 -t request.time="2017-07-04T00:01:10Z"

Report RPC returned OK
```
