# Testing guide

This assumes you already read the [development guide](development.md) to
install go, glide, and configure your git client.  All command examples are
relative to the `mixer` root directory.

Before sending pull requests you should at least make sure your changes have
passed both unit and integration tests.

Istio Mixer only merges pull requests when **all** tests are passing.

## Unit tests

* Unit tests should be fully hermetic
  - Only access resources in the test binary.
* All packages and any significant files require unit tests.
* The preferred method of testing multiple scenarios or input is
  [table driven testing](https://github.com/golang/go/wiki/TableDrivenTests)
[//]: # (TODO: Add info about presubmits here when applicable)
* Concurrent unit test runs must pass.
* See [coding conventions](coding-conventions.md).

### Run all unit tests

`make test` is the entrypoint for running the unit tests that ensures that
`GOPATH` is set up correctly.  If you have `GOPATH` set up correctly, you can
also just use `go test` directly.

```sh
cd mixer
make test  # Run all unit tests.
```