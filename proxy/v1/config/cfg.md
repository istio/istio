# Routing Rule Config Documentation
<a name="top"/>

## Table of Contents
* [Glossary & Concepts](#cfg.proto)
  * [Routing Rules](#istio.proxy.v1alpha.config.RouteRule)
    * [Match Conditions](#istio.proxy.v1alpha.config.MatchCondition)
    * [Weighted Routing to Different Destination Service Versions](#istio.proxy.v1alpha.config.DestinationWeight)
    * [HTTP Req Retries](#istio.proxy.v1alpha.config.HTTPRetry)
    * [HTTP Req Timeouts](#istio.proxy.v1alpha.config.HTTPTimeout)
    * [HTTP Fault Injection](#istio.proxy.v1alpha.config.HTTPFaultInjection)    
  * [Destination Policies](#istio.proxy.v1alpha.config.DestinationPolicy)
    * [Load Balancing](#istio.proxy.v1alpha.config.LoadBalancing)
    * [Circuit Breakers](#istio.proxy.v1alpha.config.CircuitBreaker)

<a name="cfg.proto"/>
<p align="right"><a href="#top">Top</a></p>

## Glossary & Concepts

Service is a unit of an application with a unique name that other services
use to refer to the functionality being called. Service instances are
pods/VMs/containers that implement the service.

Service versions - In a continuous deployment scenario, for a given service,
there can be multiple sets of instances running potentially different
variants of the application binary. These variants are not necessarily
different API versions. They could be iterative changes to the same service,
deployed in different environments (prod, staging, dev, etc.). Common
scenarios where this occurs include A/B testing, canary rollouts, etc. The
choice of a particular version can be decided based on various criterion
(headers, url, etc.) and/or by weights assigned to each version.  Each
service has a default version consisting of all its instances.

Source - downstream client (browser or another service) calling the
proxy/sidecar (typically to reach another service).

Destination - The remote upstream service to which the proxy/sidecar is
talking to, on behalf of the source service. There can be one or more
service versions for a given service (see the discussion on versions above).
The proxy would choose the version based on various routing rules.

Applications address only the destination service without knowledge of
individual service versions. The actual choice of the version is determined
by the proxy, enabling the application code to decouple itself from the
evolution of dependent services.

Most fields in this configuration are optional and fallback to sensible
defaults. Mandatory fields contain the word REQUIRED in the
description.

<a name="istio.proxy.v1alpha.config.RouteRule"/>
### Route Rules

Route rule provides a custom routing policy based on the source and
destination service versions and connection/request metadata.  The rule must
provide a set of conditions for each protocol (TCP, UDP, HTTP) that the
destination service exposes on its ports. The rule applies only to the ports
on the destination service for which it provides protocol-specific match
condition, e.g. if the rule does not specify TCP condition, the rule does
not apply to TCP traffic towards the destination service.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| destination | [string](#string) | optional | REQUIRED: Destination uniquely identifies the destination associated with this routing rule.  This field is applicable for hostname-based resolution for HTTP traffic as well as IP-based resolution for TCP/UDP traffic. The value MUST be a fully-qualified domain name, e.g. "my-service.default.svc.cluster.local". |
| precedence | [int32](#int32) | optional | Precedence is used to disambiguate the order of application of rules for the same destination service. A higher number takes priority. If not specified, the value is assumed to be 0.  The order of application for rules with the same precedence is unspecified. |
| match | [MatchCondition](#istio.proxy.v1alpha.config.MatchCondition) | optional | Optional match condtions to be satisfied for the route rule to be activated. If match is omitted, the route rule applies only to HTTP traffic. |
| route | [DestinationWeight](#istio.proxy.v1alpha.config.DestinationWeight) | repeated | Each routing rule is associated with one or more service version destinations (see glossary in beginning of document). Weights associated with the service version determine the proportion of traffic it receives. |
| http_req_timeout | [HTTPTimeout](#istio.proxy.v1alpha.config.HTTPTimeout) | optional | Timeout policy for HTTP requests. |
| http_req_retries | [HTTPRetry](#istio.proxy.v1alpha.config.HTTPRetry) | optional | Retry policy for HTTP requests. |
| http_fault | [HTTPFaultInjection](#istio.proxy.v1alpha.config.HTTPFaultInjection) | optional | L7 fault injection policy applies to Http traffic |

<a name="istio.proxy.v1alpha.config.MatchCondition"/>
### Match Conditions
Match condition specifies a set of criterion to be met in order for the
 route rule to be applied to the connection or HTTP request.  The
 condition provides distinct set of conditions for each protocol with
 the intention that conditions apply only to the service ports that
 match the protocol.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| source | [string](#string) | optional | Identifies the service initiating a connection or a request by its name. If specified, name MUST BE a fully qualified domain name such as foo.bar.com |
| source_tags | [MatchCondition.SourceTagsEntry](#istio.proxy.v1alpha.config.MatchCondition.SourceTagsEntry) | repeated | Identifies the source service version. The identifier is interpreted by the platform to match a service version for the source service.N.B. The map is used instead of pstruct due to lack of serialization supportin golang protobuf library (see https://github.com/golang/protobuf/pull/208) |
| tcp | [L4MatchAttributes](#istio.proxy.v1alpha.config.L4MatchAttributes) | optional | Set of layer 4 match conditions based on the IP ranges. INCOMPLETE implementation |
| udp | [L4MatchAttributes](#istio.proxy.v1alpha.config.L4MatchAttributes) | optional |  |
| http_headers | [MatchCondition.HttpEntry](#istio.proxy.v1alpha.config.MatchCondition.HttpEntry) | repeated | Set of HTTP match conditions based on HTTP/1.1, HTTP/2, GRPC request metadata, such as "uri", "scheme", "authority". The header keys are case-insensitive. |


<a name="istio.proxy.v1alpha.config.MatchCondition.HttpEntry"/>
### MatchCondition.HttpEntry


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) | optional |  |
| value | [StringMatch](#istio.proxy.v1alpha.config.StringMatch) | optional |  |


<a name="istio.proxy.v1alpha.config.MatchCondition.SourceTagsEntry"/>
### MatchCondition.SourceTagsEntry


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) | optional |  |
| value | [string](#string) | optional |  |


<a name="istio.proxy.v1alpha.config.L4MatchAttributes"/>
### L4MatchAttributes
L4 connection match attributes. Note that L4 connection matching
 support is incomplete.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| source_subnet | [string](#string) | repeated | IPv4 or IPv6 ip address with optional subnet. E.g., a.b.c.d/xx form or just a.b.c.d |
| destination_subnet | [string](#string) | repeated | IPv4 or IPv6 ip address of destination with optional subnet. E.g., a.b.c.d/xx form or just a.b.c.d. This is only valid when the destination service has several IPs and the application explicitly specifies a particular IP. |


<a name="istio.proxy.v1alpha.config.StringMatch"/>
### StringMatch
Describes how to matches a given string (exact match, prefix-based
 match or posix style regex based match). Match is case-sensitive. NOTE:
 use of regex depends on the specific proxy implementation.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| exact | [string](#string) | optional |  |
| prefix | [string](#string) | optional |  |
| regex | [string](#string) | optional |  |



<a name="istio.proxy.v1alpha.config.DestinationWeight"/>
### Weighted Routing to Different Destination Service Versions
Each routing rule is associated with one or more service versions (see
 glossary in beginning of document). Weights associated with the version
 determine the proportion of traffic it receives.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| destination | [string](#string) | optional | Destination uniquely identifies the destination service. If not specified, the value is inherited from the parent route rule. Value must be in fully qualified domain name format (e.g., "my-service.default.svc.cluster.local"). |
| tags | [DestinationWeight.TagsEntry](#istio.proxy.v1alpha.config.DestinationWeight.TagsEntry) | repeated | Service version identifier for the destination service.N.B. The map is used instead of pstruct due to lack of serialization supportin golang protobuf library (see https://github.com/golang/protobuf/pull/208) |
| weight | [int32](#int32) | optional | The proportion of traffic to be forwarded to the service version. Max is 100. Sum of weights across destinations should add up to 100. If there is only destination in a rule, the weight value is assumed to be 100. |


<a name="istio.proxy.v1alpha.config.DestinationWeight.TagsEntry"/>
### DestinationWeight.TagsEntry


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) | optional |  |
| value | [string](#string) | optional |  |


<a name="istio.proxy.v1alpha.config.HTTPRetry"/>
### HTTP Req Retries
Retry policy to use when a request fails.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| simple_retry | [HTTPRetry.SimpleRetryPolicy](#istio.proxy.v1alpha.config.HTTPRetry.SimpleRetryPolicy) | optional |  |
| custom | [Any](#google.protobuf.Any) | optional | For proxies that support custom retry policies |


<a name="istio.proxy.v1alpha.config.HTTPRetry.SimpleRetryPolicy"/>
### HTTPRetry.SimpleRetryPolicy


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attempts | [int32](#int32) | optional | Number of retries for a given request. The interval between retries will be determined automatically (25ms+). Actual number of retries attempted depends on the http_timeout |
| override_header_name | [string](#string) | optional | Downstream Service could specify retry attempts via Http header to the proxy, if the proxy supports such a feature. |


<a name="istio.proxy.v1alpha.config.HTTPTimeout"/>
### HTTP Req Timeouts
Request timeout: wait time until a response is received. Does not
 indicate the time for the entire response to arrive.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| simple_timeout | [HTTPTimeout.SimpleTimeoutPolicy](#istio.proxy.v1alpha.config.HTTPTimeout.SimpleTimeoutPolicy) | optional |  |
| custom | [Any](#google.protobuf.Any) | optional | For proxies that support custom timeout policies |


<a name="istio.proxy.v1alpha.config.HTTPTimeout.SimpleTimeoutPolicy"/>
### HTTPTimeout.SimpleTimeoutPolicy


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| timeout_seconds | [double](#double) | optional | Timeout for a HTTP request. Includes retries as well. Unit is in floating point seconds. Default 15 seconds. Specified in seconds.nanoseconds format |
| override_header_name | [string](#string) | optional | Downstream service could specify timeout via Http header to the proxy, if the proxy supports such a feature. |


<a name="istio.proxy.v1alpha.config.DestinationPolicy"/>
### Destination Policies
DestinationPolicy declares policies that determine how to handle traffic for a
 destination service (load balancing policies, failure recovery policies such
 as timeouts, retries, circuit breakers, etc).  Policies are applicable per
 individual service versions. ONLY ONE policy can be defined per service version.

 Note that these policies are enforced on client-side connections or
 requests, i.e., enforced when the service is opening a
 connection sending a request via the proxy to the destination.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| destination | [string](#string) | optional | REQUIRED. Service name for which the service version is defined. The value MUST be a fully-qualified domain name, e.g. "my-service.default.svc.cluster.local". |
| tags | [DestinationPolicy.TagsEntry](#istio.proxy.v1alpha.config.DestinationPolicy.TagsEntry) | repeated | Service version destination identifier for the destination service. The identifier is qualified by the destination service name, e.g. version "env=prod" in "my-service.default.svc.cluster.local".N.B. The map is used instead of pstruct due to lack of serialization supportin golang protobuf library (see https://github.com/golang/protobuf/pull/208) |
| load_balancing | [LoadBalancing](#istio.proxy.v1alpha.config.LoadBalancing) | optional | Load balancing policy |
| circuit_breaker | [CircuitBreaker](#istio.proxy.v1alpha.config.CircuitBreaker) | optional | Circuit breaker policy |
| custom | [Any](#google.protobuf.Any) | optional | Other custom policy implementations |


<a name="istio.proxy.v1alpha.config.DestinationPolicy.TagsEntry"/>
### DestinationPolicy.TagsEntry


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) | optional |  |
| value | [string](#string) | optional |  |


<a name="istio.proxy.v1alpha.config.LoadBalancing"/>
### Load Balancing
Load balancing policy to use when forwarding traffic.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [LoadBalancing.SimpleLBPolicy](#istio.proxy.v1alpha.config.LoadBalancing.SimpleLBPolicy) | optional |  |
| custom | [Any](#google.protobuf.Any) | optional | Custom LB policy implementations |


<a name="istio.proxy.v1alpha.config.LoadBalancing.SimpleLBPolicy"/>
### LoadBalancing.SimpleLBPolicy
Common load balancing policies supported in Istio service mesh.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ROUND_ROBIN | 0 |  |
| LEAST_CONN | 1 |  |
| RANDOM | 2 |  |


<a name="istio.proxy.v1alpha.config.CircuitBreaker"/>
### Circuit Breakers
Circuit breaker configuration.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| simple_cb | [CircuitBreaker.SimpleCircuitBreakerPolicy](#istio.proxy.v1alpha.config.CircuitBreaker.SimpleCircuitBreakerPolicy) | optional |  |
| custom | [Any](#google.protobuf.Any) | optional | For proxies that support custom circuit breaker policies. |


<a name="istio.proxy.v1alpha.config.CircuitBreaker.SimpleCircuitBreakerPolicy"/>
### CircuitBreaker.SimpleCircuitBreakerPolicy


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| max_connections | [int32](#int32) | optional | Maximum number of connections to a backend. |
| http_max_pending_requests | [int32](#int32) | optional | Maximum number of pending requests to a backend. |
| http_max_requests | [int32](#int32) | optional | Maximum number of requests to a backend. |
| sleep_window | [double](#double) | optional | Minimum time the circuit will be closed. In floating point seconds format. |
| http_consecutive_errors | [int32](#int32) | optional | Number of 5XX errors before circuit is opened. |
| http_detection_interval_seconds | [double](#int32) | optional | Interval for checking state of hystrix circuit. |
| http_max_requests_per_connection | [int32](#int32) | optional | Maximum number of requests per connection to a backend. |
| http_max_ejection_percent | [int32](#int32) | optional | Maximum percentage of hosts in the destination service that can be ejected due to circuit breaking. Defaults to 10%. |


<a name="istio.proxy.v1alpha.config.HTTPFaultInjection"/>
### Fault Injection
Faults can be injected into the API calls by the proxy, for testing the
 failure recovery capabilities of downstream services.  Faults include
 aborting the Http request from downstream service, delaying the proxying of
 requests, or both. MUST specify either delay or abort or both.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| delay | [HTTPFaultInjection.Delay](#istio.proxy.v1alpha.config.HTTPFaultInjection.Delay) | optional | Delay requests before forwarding, emulating various failures such as network issues, overloaded upstream service, etc. |
| abort | [HTTPFaultInjection.Abort](#istio.proxy.v1alpha.config.HTTPFaultInjection.Abort) | optional | Abort Http request attempts and return error codes back to downstream service, giving the impression that the upstream service is faulty. N.B. Both delay and abort can be specified simultaneously. Delay and Abort are independent of one another. For e.g., if Delay is restricted to 5% of requests while Abort is restricted to 10% of requests, the 10% in abort specification applies to all requests directed to the service. It may be the case that one or more requests being aborted were also delayed. |
| headers | [HTTPFaultInjection.HeadersEntry](#istio.proxy.v1alpha.config.HTTPFaultInjection.HeadersEntry) | repeated | Only requests with these Http headers will be subjected to fault injection |


<a name="istio.proxy.v1alpha.config.HTTPFaultInjection.Abort"/>
### HTTPFaultInjection.Abort
Abort Http request attempts and return error codes back to downstream
 service.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| percent | [float](#float) | optional | percentage of requests to be aborted with the error code provided. |
| grpc_status | [string](#string) | optional |  |
| http2_error | [string](#string) | optional |  |
| http_status | [int32](#int32) | optional |  |


<a name="istio.proxy.v1alpha.config.HTTPFaultInjection.Delay"/>
### HTTPFaultInjection.Delay
MUST specify either a fixed delay or exponential delay. Exponential
 delay is unsupported at the moment.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| percent | [float](#float) | optional | percentage of requests on which the delay will be injected |
| fixed_delay_seconds | [double](#double) | optional | Add a fixed delay before forwarding the request. Delay duration in seconds.nanoseconds |
| exponential_delay_seconds | [double](#double) | optional | Add a delay (based on an exponential function) before forwarding the request. mean delay needed to derive the exponential delay values |
