# The Istio Manager #
[![Build Status](https://travis-ci.org/istio/manager.svg?branch=master)](https://travis-ci.org/istio/manager)

The Istio Manager is used to configure Istio and propagate configuration to the other components of the system, including the Istio mixer and the Istio proxy.

[Contributing to the project](./CONTRIBUTING.md)

## Filing issues ##

If you have a question about the Istio Manager or have a problem using it, please
[file an issue](https://github.com/istio/manager/issues/new).

## Build instructions ##

We are using [Bazel 0.4.2](https://bazel.io) on Linux to build Istio Manager:

    bazel build :all

You can also use regular `go test` and `go build` but keep in mind
that we are building against a specific version of [Kubernetes `client-go`](https://github.com/kubernetes/client-go).
Place the repository clone into your `$GOROOT`:

    mkdir -p $GOROOT/src/istio.io
    ln -s <istio.io/manager repo> $GOROOT/src/istio.io
    go test ./... -v

## Test environment ##

We are using [minikube](https://github.com/kubernetes/minikube) to develop for the Kubernetes environment.
To let Bazel sandbox access the cluster, please add a symbolic link to your configuration:

    ln -s ~/.kube/config platform/kube/
    bazel test :all

