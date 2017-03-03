# Istio Manager testing infrastructure

All contributions to Istio Manager should supply tests that cover new features and/or bug fixes.
We strive to improve and maintain quality of the code with up-to-date samples, good coverage of the code, and code analyzers to detect problems in the code early.

## Code linters

We require that Istio Manager code contributions pass all linters defined by [the check script](bin/check.sh).

## Unit tests

We follow Golang unit testing practice of creating `source_test.go` next to `source.go` files that provide sufficient coverage of the functionality. These tests can also be run using `bazel` with additional strict dependency declarations.

For tests that require Kubernetes access, we rely on the client libraries and `.kube/config` file that needs to be linked into your repository directory as `platform/kube/config`. Kubernetes tests use this file to authenticate and access the cluster.

For tests that require systems integration, such as invoking the proxy with a special configuration, we capture the desired output as golden artifacts and save the artifacts in the repository. Validation tests compare generated output against the desired output. For example, [Envoy configuration test data](proxy/envoy/testdata) contains auto-generated proxy configuration. If you make changes to the config generation, you also need to create or update the golden artifact in the same pull request.

## Integration tests

Istio Manager runs end-to-end tests as part of the presubmit check. The test driver is a [Golang program](test/integration) that creates a temporary namespace, deploys Istio components, send requests from apps in the cluster, and checks that traffic obeys the desired routing policies. The end-to-end test is entirely hermetic: test applications and Istio Manager docker images are generated on each run. This means you need to have a docker registry to host your images, which then needs to be passed with `-h` flag. The test driver is invoked using [the e2e script](bin/e2e.sh).

## Debug docker images

Istio Manager produces debug images in addition to default images. These images have suffix `_debug` and include additional tools such as `curl` in the base image and debug-enabled Envoy builds.

## Test logging

Istio Manager uses [glog](https://godoc.org/github.com/golang/glog) library for all its logging. We encourage extensive logging at the appropriate log levels. As a hint to log level selection, level 10 is the most verbose (Kubernetes will show all its HTTP requests), level 2 is used by default in the integration tests, level 4 turns on extensive logging in the proxy.

