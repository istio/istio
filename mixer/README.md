# Mixer

## Deprecation notice

As of Istio 1.5, Mixer is deprecated. The functionality provided by Mixer is
being moved into the Envoy proxies. Use of Mixer with Istio will only be
supported through the 1.7 release of Istio.

## Mixer's future

Because of the extensive feedback we received from the community about the
difficulties encountered with Mixer, we are making a substantial change to
Istio's architecture. By taking advantage of the work being done in the
[WebAssembly](https://webassembly.org) community, we are working to move the
functionality that formerly lived in Mixer into the Envoy proxy itself.

What will this do for the Istio user? It will greatly reduce complexity, by:
- Reducing the number of components to install
- Reducing the number of CRDs Istio installs
- Making Envoy simpler to run with fewer components to manage and scale

For partners integrating with Istio, it standardizing the method of integrating
external components with the broader Envoy community.

To read more about the changes (which we're tentatively calling Extensions v2),
please see
[this doc](
https://docs.google.com/document/d/1x5XeKWRdpFPAy7JYxiTz5u-Ux2eoBQ80lXT6XYjvUuQ/edit#heading=h.8kpssnjs5pqw)
. Note you must follow [these directions](
https://github.com/istio/community/blob/master/CONTRIBUTING.md#design-documents)
to get access to Istio design documents.

## Obsolete Mixer Documentation

Mixer enables extensible policy enforcement and control within the Istio service
mesh. It is responsible for insulating the proxy (Envoy) from details of the
current execution environment and the intricacies of infrastructure backends.

Mixer provides three distinct features:

- **Precondition Checking**. Enables callers to verify a number of preconditions
  before responding to an incoming request from a service consumer.
  Preconditions can include whether the service consumer is properly
  authenticated, is on the service's whitelist, passes ACL checks, and more.

- **Quota Management**. Enables services to allocate and free quota on a number
  of dimensions, Quotas are used as a relatively simple resource management tool
  to provide some fairness between service consumers when contending for limited
  resources.

- **Telemetry Reporting**. Enables services to produce logging, monitoring,
  tracing and billing streams intended for the service producer itself as well
  as for its consumers.

Learn more about Mixer
[here](https://istio.io/docs/concepts/policies-and-telemetry/).

Mixer's
[Adapter Developer's Guide](https://github.com/istio/istio/wiki/Mixer-Out-Of-Process-Adapter-Dev-Guide)
presents everything you need to know about extending Mixer to provide support
for new backends through the development of new
[adapters](https://istio.io/docs/concepts/policies-and-telemetry/#adapters).

Mixer's
[Template Developer's Guide](https://github.com/istio/istio/wiki/Mixer-Template-Dev-Guide)
presents everything you need to know about you can create new templates to define
whole new categories of adapters.
