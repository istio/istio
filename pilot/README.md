# Istio Manager #
[![Build Status](https://travis-ci.org/istio/manager.svg?branch=master)](https://travis-ci.org/istio/manager)
[![Go Report Card](https://goreportcard.com/badge/github.com/istio/manager)](https://goreportcard.com/report/github.com/istio/manager)

The Istio Manager is used to configure Istio and propagate configuration to the
other components of the system, including the Istio mixer and the Istio proxy mesh.

[Contributing to the project](./CONTRIBUTING.md)

## Filing issues ##

If you have a question about the Istio Manager or have a problem using it, please
[file an issue](https://github.com/istio/manager/issues/new).

## Build instructions ##

We are using [Bazel 0.4.3](https://bazel.io) to build Istio Manager:

    bazel build //cmd/...

_Note_: Due to issues with case-insensitive file systems, macOS is not
supported at the moment by Bazel Go rules.

Bazel uses `BUILD` files to keep track of dependencies between sources.
If you add a new source file or change the imports, please run the following command
to update all `BUILD` files:

    gazelle -go_prefix "istio.io/manager" --mode fix -repo_root .

Gazelle binary is located in Bazel external tools:

    external/io_bazel_rules_go_repository_tools/bin/gazelle

## Test environment ##

Manager tests require access to a Kubernetes cluster. We recommend Kubernetes 
version >=1.5.2 due to its improved support for Third-Party Resources. Each
test operates on a temporary namespace and deletes it on completion.  Please
configure your `kubectl` to point to a development cluster before building or
invoking the tests and add a symbolic link to your
repository pointing to Kubernetes cluster credentials:

    ln -s ~/.kube/config platform/kube/

If you are using GKE, please make sure you are using static client
certificates before fetching cluster credentials:

    gcloud config set container/use_client_certificate True

To run the tests:

    bazel test //...

## Build instructions without Bazel ##

Bazel does not preclude you from using `go` tool in development. You should
check out your repository clone `$REPO_PATH` into `$GOPATH` (e.g.
`$GOPATH/src/istio.io/manager`) and then run `bazel build //cmd/...` to let Bazel
fetch all dependencies. Then run this script in the repository root:

    bin/init.sh
        
## Docker images ##

We provide Bazel targets to output Istio runtime images:

    bazel run //docker:runtime
    
The image includes Istio Proxy and Istio Manager.

