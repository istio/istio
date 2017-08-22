# Build instructions for Linux

See also [Istio Development](https://github.com/istio/istio/blob/master/devel/README.md) for setup and general tips.

(For Windows and Mac, we recommend using a Linux virtual machine and/or [Vagrant-specific build instructions](build-vagrant.md) or [minikube build pod](minikube.md); Go code compiles on Mac but docker and proxy tests will fail on Mac)

We are using [Bazel 0.5.2](https://github.com/bazelbuild/bazel/releases) as the main build system in Istio Pilot. The following command builds all targets in Istio Pilot:

To avoid a `missing input file '//platform/kube:config'` error you need to `ln -s ~/.kube/config platform/kube/` or `make kubeconfig`, see [testing](testing.md) for details/why.

    make build

Bazel uses `BUILD` files to keep track of dependencies between sources.  If you
add a new source file or change the imports  please run the following command
in the repository root to update all `BUILD` files:

    make gazelle

Data dependencies such as the ones used by tests require manual declaration in
the `BUILD` files.

## Go tooling compatibility

Istio Pilot requires Go1.8+ toolchain.

Bazel build environment is compatible with the standard Golang tooling, except you need to vendorize all dependencies in Istio Pilot. If you have successfully built with Bazel, run the following script to put dependencies fetched by Bazel into `vendor` directory and install the Go1.8+ toolchain:

    make setup

After running this command, you should be able to use all standard go tools:

    make build
    make test

_Note_: these commands assume you have placed the repository clone into `$GOPATH`.
