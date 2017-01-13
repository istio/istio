# Istio Manager #
[![Build Status](https://travis-ci.org/istio/manager.svg?branch=master)](https://travis-ci.org/istio/manager)

The Istio Manager is used to configure Istio and propagate configuration to the
other components of the system, including the Istio mixer and the Istio proxy mesh.

[Contributing to the project](./CONTRIBUTING.md)

## Filing issues ##

If you have a question about the Istio Manager or have a problem using it, please
[file an issue](https://github.com/istio/manager/issues/new).

## Build instructions ##

We are using [Bazel 0.4.3](https://bazel.io) to build Istio Manager:

    bazel build //...

_Note_: Due to issues with case-insensitive file systems, macOS is not
supported at the moment by Bazel Go rules.

Bazel uses `BUILD` files to keep track of dependencies between sources.
If you add a new source file or change the imports, please run the following command
to update all `BUILD` files:

    gazelle -go_prefix "istio.io/manager" --mode fix -repo_root .

Gazelle binary is located in Bazel external tools:

    external/io_bazel_rules_go_repository_tools/bin/gazelle

## Build instructions without Bazel ##
Bazel does not preclude you from using `go` tool in development.
Place your repository into `$GOPATH`:

    mkdir -p $GOPATH/src/istio.io
    ln -s <istio.io/manager repo> $GOPATH/src/istio.io
    go test ./... -v

You would need to `go get` each dependency at the right SHA (see `WORKSPACE` file) for
the build to succeed.

## Test environment ##

Manager tests require an access to a Kubernetes cluster. Each test creates a
temporary namespace and deletes it on completion.  Please configure your
`kubectl` to point to a development cluster before invoking the tests.

To let Bazel sandboxes access the cluster, please add a symbolic link to your
repository pointing to your Kubernetes configuration file:

    ln -s ~/.kube/config platform/kube/
    bazel test //...

_Note_: Due to a well-known issue, the namespaces are not deleted completely
after running the tests and permanently reside in a terminating state
(see https://github.com/istio/manager/issues/15).
