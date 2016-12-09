# The Istio Manager #

The Istio manager is used to configure Istio and propagate configuration to the other components of the system, including the Istio mixer and the Istio proxy.

[Contributing to the project](./CONTRIBUTING.md)

## Filing issues ##

If you have a question about the Istio manager or have a problem using it, please
[file an issue](https://github.com/istio/manager/issues/new).

## Build instructions ##

We are using [Bazel 0.4.2](https://bazel.io) to build Istio manager:

    bazel build :all
    bazel test :all

You can also use regular `go test` and `go build` but keep in mind
that we are building against a specific version of [Kubernetes `client-go`](https://github.com/kubernetes/client-go).
Place the repository clone into your `$GOROOT`:
    
    mkdir -p $GOROOT/src/istio.io
    ln -s <istio.io/manager repo> $GOROOT/src/istio.io
    go test ./... -v

## Test environment ##

We are using [minikube](https://github.com/kubernetes/minikube) to develop for the Kubernetes environment.
Obtain docker daemon environment variables by running:

    eval $(minikube docker-env)

You can use bazel to build docker images for the agent, and push it to minikube:

    bazel run //docker:kube_agent-docker
    kubectl run --image bazel/docker:kube_agent-docker example
