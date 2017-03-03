# Istio Manager #
[![Build Status](https://travis-ci.org/istio/manager.svg?branch=master)](https://travis-ci.org/istio/manager)
[![Go Report Card](https://goreportcard.com/badge/github.com/istio/manager)](https://goreportcard.com/report/github.com/istio/manager)
[![GoDoc](https://godoc.org/github.com/istio/manager?status.svg)](https://godoc.org/github.com/istio/manager)
[![codecov.io](https://codecov.io/github/istio/manager/coverage.svg?branch=master)](https://codecov.io/github/istio/manager?branch=master)

Istio Manager is used to configure Istio and propagate configuration to the
other components of the system, including the Istio mixer and the Istio proxies. 

[Contributing to the project](./CONTRIBUTING.md)

## Filing issues ##

If you have a question about the Istio Manager or have a problem using it, please
[file an issue](https://github.com/istio/manager/issues/new).

## Getting started ##

Istio Manager [design](doc/design.md) gives an architectural overview of the manager components - cluster platform abstractions, service model, and the proxy controllers.

If you are interested in contributing to the project, please take a look at the [build instructions](doc/build.md) and the [testing infrastructure](doc/testing.md).

