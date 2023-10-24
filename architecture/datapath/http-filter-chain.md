# HTTP filter chain organization principles
 
A request in Envoy undergoes a sequence of the processing steps implemented using _HTTP filters_.
The effects of each step are visible to the subsequent steps, e.g. authorization enforcement
uses the authentication attributes. A mesh implementation must maintain a consistent order through
the upgrades to avoid unintended datapath changes. To define such order, the following principles
are followed to create a formal contract on the xDS filter chain organization.

## Processing phases

At a high-level, there are four broad phases that are expected during processing:

* _Routing_: selection of the route entry and the upstream. This is normally done via xDS configuration without the filters.

* _Application security_: a policy enforcement point for applying HTTP policies. This generally includes
  authentication, authorization, and secure attribute derivation.

* _Traffic management_: transformation of the request headers and body. This generally includes protocol
  transcoders, fault injection, load balancing.

* _Telemetry_: collection of the request telemetry. This is normally done via access loggers, metrics, and tracers.

Changing the order of the phases may cause un-intended consequences. Therefore, as a general guideline, Istio 
must follow the above order in the filter chain organization. Some examples of potential issues with a different order:

* Changing routing after the authorization enforcement may allow traffic bypass the authorization policy.

* Applying traffic transformation before the policy enforcement causes denied requests consume proxy resources,
  and opens a possibility for an resource exhaustion vulnerability. 
 
* Collecting telemetry before the traffic management will report inconsistent data with the upstream servers.

### Application security 

Istio normally performs routing via xDS configuration, external to the filter chain configuration. The only exceptions
are JWT-claim based routing and Wasm extensions. Therefore, these two filters must be placed the earliest in the chain,
preceding any authorization enforcement filters. One special exception is 

