![Mixer](doc/logo.png)
<h1>The Istio Mixer</h1>
[![GoDoc](https://godoc.org/github.com/istio/mixer?status.svg)](http://godoc.org/github.com/istio/mixer) [![Build Status](https://travis-ci.org/istio/mixer.svg?branch=master)](https://travis-ci.org/istio/mixer)
		
The Istio mixer provides three distinct features:

- *Precondition Checking*. The `Check` method enables
the caller to verify a number of preconditions before
responding to an incoming request from a service consumer.
Preconditions can include whether the service consumer
is on the service's whitelist, whether the service consumer
has the right access privilege, and more.

- *Telemetry Reporting*. The `Report` method enables services
to produce logging and monitoring streams intended for
service consumers.

- *Quota Management*. The `Quota` method enables services
to allocate and free quota on a number of dimensions, Quotas
are used as a relatively simple resource management tool to
provide some fairness between service consumers when contending
for limited service resources.

To learn more...

- [Mixer user guide](doc/userGuide/README.md)
- [Using the mixer API](doc/api.md)
- [Contributing to the project](./CONTRIBUTING.md)

### Filing issues

If you have a question about the Istio mixer or have a problem using it, please
[file an issue](https://github.com/istio/mixer/issues/new).
