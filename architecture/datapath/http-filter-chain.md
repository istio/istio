# HTTP filter chain organization principles
 
A request in Envoy undergoes a sequence of the processing steps implemented using _HTTP filters_.
The effects of each step are visible to the subsequent steps, e.g. authorization enforcement
uses the authentication attributes. A mesh implementation must maintain a consistent order through
the upgrades to avoid unintended datapath changes. To define such order, the following principles
are followed to create a formal contract on the xDS filter chain organization.

## Processing phases

At a high-level, there are four broad phases that are expected during processing:

* _Routing_: selecting the route entry and the upstream. This is normally done via xDS configuration without the filters.

* _Application security_: a policy enforcement point for applying HTTP policies. This generally includes
  authentication, authorization, and secure attribute derivation.

* _Traffic management_: transformation of the request headers and body. This generally includes protocol
  transcoders, fault injection, load balancing.

* _Telemetry_: collection of the request telemetry. This is normally done via access loggers, metrics, and tracers.

Changing the order of the phases may cause unintended consequences. Therefore, as a general guideline, Istio 
must follow the above order in the filter chain organization. 

Some examples of potential issues caused by violating the order:

* Changing routing after the authorization enforcement may allow a request to bypass the authorization policy.

* Applying traffic transformation before the policy enforcement may amplify proxy resource consumption for denied requests. 
 
* Collecting telemetry before the traffic management may report inconsistent data with the upstream servers.

### Routing phase

Istio configures routing using xDS routing configuration and does not rely on additional HTTP filters to determine the route and the upstream cluster.

### Application security

Istio defines two steps within this phase:

* _Authentication_: extracting the secure attributes from the request to influence policy decision and route selection.
* _Authorization_: enforcement of the policies.

Authentication should precede authorization so that the policy takes into account the full set of attributes, and also because Istio allows authentication filters to affect routing.
For example, JWT authentication filters can influence the routing decision via a [claim-based routing feature](https://istio.io/latest/docs/tasks/security/authentication/jwt-route/).

A special consideration should be applied to the CORS policy filter. This filter short-circuits the preflight requests by explicitly allowing them. Because browsers are [not supplying the additional authentication headers](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS#examples_of_access_control_scenarios), placing CORS after JWT authentication causes unexpected denial of the preflight requests. Therefore,
the correct placement for CORS is to precede JWT and other authentication filters.

### Traffic management

In Istio, traffic management features include the fault injection, grpc-web transcoding, persistent session load balancing, and mTLS ALPN overriding. These features should be logically placed next in the chain, after the application security filters.

### Telemetry

Telemetry filters are expected to observe the traffic and typically have no visible effect on the data path. Therefore, these filters in practice can be placed anywhere in the chain, but should be placed last to capture the final request form.

## Istio filter order specification

If we follow the principles outlined above, the following filter chain order should take into account the entirety of Istio HTTP filter extensions:

| Filter | Phase | Purpose |
| --- | --- | --- |
| `metadata_exchange` | Application Security / Authentication | Peer metadata attributes (currently insecure) |
| `wasm`              | Application Security / Authentication | Wasm extension per [AUTHN phase](https://istio.io/latest/docs/reference/config/proxy_extensions/wasm-plugin/#PluginPhase) |
| `cors`              | Application Security / Authentication | Allow CORS requests per CORS policy |
| `jwt_authn`         | Application Security / Authentication | JWT attributes and routing |
| `wasm`              | Application Security / Authorization | Wasm extension per [AUTHZ phase](https://istio.io/latest/docs/reference/config/proxy_extensions/wasm-plugin/#PluginPhase) |
| `ext_authz`         | Application Security / Authorization | Istio CUSTOM authorization |
| `rbac`              | Application Security / Authorization | Istio local authorization |
| `grpc_web`          | Traffic Management | grpc-web transcoding |
| `istio_alpn`        | Traffic Management | ALPN upstream override |
| `fault`             | Traffic Management | Fault injection |
| `stateful_session`  | Traffic Management | Upstream host selection |
| `wasm`              | Telemetry  | Wasm extension per [STATS phase](https://istio.io/latest/docs/reference/config/proxy_extensions/wasm-plugin/#PluginPhase) |
| `grpc_stats`        | Telemetry  | gRPC streaming counts telemetry attributes |

## Deviations from Istio 1.20

Istio 1.20 and lower deviate from the above ordering in the following ways:

### CORS placement

`cors` filter is placed after `fault` and before `istio_stats`. It should be placed immediately before `jwt_authn`.

### Istio CUSTOM placement

`ext_authz` filter is placed after `metadata_exchange` and before `wasm` authentication. It should be placed immediately before `rbac`.

### Telemetry interleaving

`grpc_stats` and `wasm` filters are placed before the traffic management filters. They should be placed last.

`istio_stats` and `stackdriver` filters are not true filters and should be converted to the appropriate Envoy access logger interfaces.


