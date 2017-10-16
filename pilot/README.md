# Istio Pilot #
[Build Status](https://prow.istio.io/?job=pilot-postsubmit)
[![Go Report Card](https://goreportcard.com/badge/github.com/istio/pilot)](https://goreportcard.com/report/github.com/istio/pilot)
[![GoDoc](https://godoc.org/github.com/istio/pilot?status.svg)](https://godoc.org/github.com/istio/pilot)
[![codecov.io](https://codecov.io/github/istio/pilot/coverage.svg?branch=master)](https://codecov.io/github/istio/pilot?branch=master)

Istio Pilot provides platform-independant service discovery, and exposes an
interface to configure rich L7 routing features such as label based routing
across multiple service versions, fault injection, timeouts, retries,
circuit breakers. It translates these configurations into sidecar-specific
configuration and dynamically reconfigures the sidecars in the service mesh
data plane. Platform-specific eccentricities are abstracted and a
simplified service discovery interface is presented to the sidecars based
on the Envoy data plane API.

Please see
[Istio's traffic management concepts](https://istio.io/docs/concepts/traffic-management/overview.html)
to learn more about the design of Pilot and the capabilities it provides.

Istio Pilot [design](doc/design.md) gives an architectural overview of its
internal components - cluster platform abstractions, service model, and the
proxy controllers.


To learn how you can contribute to Istio Pilot, please
see the Istio
[contribution guidelines](https://github.com/istio/istio/blob/master/CONTRIBUTING.md).

# Quick start

1. *Install Bazel:* [Bazel 0.6.1](https://github.com/bazelbuild/bazel/releases/tag/0.6.1) or
  higher. Debian packages are available on Linux. For OS X users, bazel is
  available via Homebrew.
  > NOTE 1: Bazel tool is still maturing, and as such has several issues that
  > makes development hard. While setting up Bazel is mostly smooth, it is
  > common to see cryptic errors that prevent you from getting
  > started. Deleting and restarting everything generally helps.

  > NOTE 2: If you are developing for the Kubernetes platform, for end-to-end
  > integration tests, you need access to a working Kubernetes cluster.

1. *Setup:* Run `make setup`. It installs the required tools and
[vendorizes](https://golang.org/cmd/go/#hdr-Vendor_Directories) 
the dependencies.

1. Write code using your favorite IDE. Make sure to format code and run
   it through the Go linters, before submitting a PR.
   `make fmt` to format the code.
   `make lint` to run the linters defined in bin/check.sh

   If you add any new source files or import new packages in
   existing code, make sure to run `make gazelle` to update the Bazel BUILD
   files.

1. *Build:* Run `make build` to compile the code.

1. *Unit test:* Run `make test` to run unit tests.
   > NOTE: If you are running on OS X, //proxy/envoy:go_default_test will
   > fail. You can ignore this failure.

1. *Dockerize:* Run `make docker HUB=docker.io/<username> TAG=<sometag>`. 
This will build a docker container for Pilot, the sidecar, and other 
utilities, and push them to Docker hub.

1. *Integration test:* Run `make e2etest HUB=docker.io/<username> TAG=<sometag>` 
with same image tags as the one you used in the dockerize stage. This step will
run end to end integration tests on Kubernetes.

Detailed instructions for testing are available [here](doc/testing.md).
