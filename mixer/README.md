# Mixer

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
