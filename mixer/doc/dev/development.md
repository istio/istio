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

## Testing

Similar to Pilot, some tests for Mixer requires access to Kubernetes cluster
(version 1.7.0 or higher). Please configure your `kubectl` to point to a development
cluster (e.g. minikube) before building or invoking the tests and add a symbolic link
to your repository pointing to Kubernetes cluster credentials:

    ln -s ~/.kube/config testdata/kubernetes/

_Note1_: If you are running Bazel in a VM (e.g. in Vagrant environment), copy
the kube config file on the host to testdata/kubernetes instead of symlinking it,
and change the paths to minikube certs.

    cp ~/.kube/config testdata/kubernetes/
    sed -i 's!/Users/<username>!/home/ubuntu!' testdata/kubernetes/config

Also, copy the same file to `/home/ubuntu/.kube/config` in the VM, and make
sure that the file is readable to user `ubuntu`.

See also [Pilot's guide of setting up test environment](https://github.com/istio/pilot/blob/master/doc/testing.md).