# Build instructions

See [Istio Development](https://github.com/istio/istio/blob/master/devel/README.md) for build instructions. Specific points for Istio/Pilot development:
* We use   [0.5.2](https://github.com/bazelbuild/bazel/releases/tag/0.5.2) version of Bazel.
* To perform the initial setup of the project, we run `make setup`. It installs the required tools and  [vendorizes](https://golang.org/cmd/go/#hdr-Vendor_Directories) the dependencies.
