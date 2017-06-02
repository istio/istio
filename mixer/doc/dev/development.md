# Development Guide

Please see the [Istio development guide](https://github.com/istio/istio/blob/master/devel/README.md) 
for the common Istio-wide development guidelines.

After which here are other Mixer specific docs you should look at:

- [Writing Mixer adapters](./adapters.md)

## Using Mixer

You will need to edit
[`testdata/configroot/scopes/global/adapters.yml`](../../testdata/configroot/scopes/global/adapters.yml)
to setup kubeconfig per the instructions at the end of the file

After you've done the above and once you've built the source tree, you can run Mixer in a basic mode using:

```shell
./bazel-bin/cmd/server/mixs server \
  --configStoreURL=fs://$(pwd)/testdata/configroot \
  --alsologtostderr
```

You can also run a simple client to interact with the server:

```shell
./bazel-bin/cmd/client/mixc check -a target.service=f.default.svc.cluster.local \
  --string_attributes source.uid=kubernetes://xyz.default
Check RPC returned OK
```
