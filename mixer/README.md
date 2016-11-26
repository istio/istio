<center>
![Mixer](docs/logo.png)
<h1>The Istio Mixer</h1>
</center>

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

- [Building the Mixer](docs/building.md)
- [Deploying an Instance](docs/deploying.md)
- [Using the API](docs/api.md)