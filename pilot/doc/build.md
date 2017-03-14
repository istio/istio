# Build instructions for Linux

(For Windows and Mac, we recommend using a Linux virtual machine and/or [Vagrant-specific build instructions](build-vagrant.md); Go code compiles on Mac but docker and proxy tests will fail on Mac)

Install additional build dependencies before trying to build.

    bin/install-prereqs.sh

We are using [Bazel 0.4.4](https://github.com/bazelbuild/bazel/releases) as the main build system in Istio Manager. The following command builds all targets in Istio Manager:

    bazel build //...

Bazel uses `BUILD` files to keep track of dependencies between sources.  If you
add a new source file, change the imports, or add a data depenency, please run the following command
to update all `BUILD` files:

    gazelle -go_prefix "istio.io/manager" -mode fix -repo_root .

[Gazelle tool](https://github.com/bazelbuild/rules_go/tree/master/go/tools/gazelle) to automatically generate `BUILD` files ships with Bazel and is located in Bazel workspace cache:

    $HOME/.cache/bazel/_bazel_<username>/<somelongfoldername>/external/io_bazel_rules_go_repository_tools/bin/gazelle

_Note_: If you cannot find the gazelle binary in the path mentioned above,
try to update the mlocate database and run `locate gazelle`.

## Go tooling compatibility

Istio Manager requires Go1.8+ toolchain.

Bazel build environment is compatible with the standard Golang tooling, except you need to vendorize all dependencies in Istio Manager. If you have successfully built with Bazel, run the following script to put dependencies fetched by Bazel into `vendor` directory:

    bin/init.sh

After running this command, you should be able to use all standard go tools:

    go generate istio.io/manager/...
    go build istio.io/manager/...
    go test -v istio.io/manager/...

_Note_: these commands assume you have placed the repository clone into `$GOPATH`.
