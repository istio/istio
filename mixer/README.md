# Mixer
![Mixer](doc/logo.png)

[![GoDoc](https://godoc.org/github.com/istio/mixer?status.svg)](https://godoc.org/github.com/istio/mixer)
[![Build Status](https://testing.istio.io/buildStatus/icon?job=mixer/postsubmit)](https://testing.istio.io/job/mixer/)
[![Go Report Card](https://goreportcard.com/badge/github.com/istio/mixer)](https://goreportcard.com/report/github.com/istio/mixer)
[![codecov.io](https://codecov.io/github/istio/mixer/coverage.svg?branch=master)](https://codecov.io/github/istio/mixer?branch=master)

Mixer provides the foundation of the Istio service mesh
design. It is responsible for insulating Envoy and
Istio-based services from details of the current execution
environment, as well as to implement the control policies
that Istio supports.

Mixer provides three distinct features:

- **Precondition Checking**. Enables callers to verify a number of preconditions before responding to an incoming request from a service consumer. 
Preconditions can include whether the service consumer is properly authenticated, is on the service's whitelist, passes ACL checks, and more.

- **Quota Management**. Enables services to allocate and free quota on a number of dimensions, Quotas are used as a relatively simple resource management 
tool to provide some fairness between service consumers when contending for limited resources.

- **Telemetry Reporting**. Enables services to produce logging, monitoring, tracing and billing streams intended for the service producer itself as well as 
for its consumers.

Learn more about Mixer [here](https://istio.io/concepts/policy-and-control/mixer.html).

Please see [istio.io](https://istio.io)
to learn about the overall Istio project and how to get in touch with us. To learn how you can
contribute to any of the Istio components, including Mixer, please 
see the Istio [contribution guidelines](https://github.com/istio/istio/blob/master/CONTRIBUTING.md).

Mixer's [developer's guide](doc/dev/development.md) presents everything
you need to know to create, build, and test code for Mixer.
