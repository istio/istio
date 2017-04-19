---
bodyclass: docs
headline: 'Routing & Traffic Management'
layout: docs
title: Routing & Traffic Management
type: markdown
sidenav: doc-side-reference-nav.html
---

## Overview

Istio provides a simple Domain-specific language (DSL) to
control how API calls and layer-4 traffic flow across various
microservices in the application deployment. The DSL is a
[YAML mapping](../reference/writing-config.md) of a
[protobuf](https://developers.google.com/protocol-buffers/docs/proto3)
schema documented [here](https://github.com/istio/api/blob/master/proxy/v1/config/cfg.md).
The DSL allows the operator to
configure service level properties such as circuit breakers, timeouts,
retries, as well as set up common continuous deployment tasks such as
canary rollouts, A/B testing, staged rollouts with %-based traffic splits,
etc.

For example, a simple rule to send 100% of incoming traffic for a "reviews" microservice
to version "v1" can be described using the Rules DSL as follows:

```yaml
destination: reviews.default.svc.cluster.local
route:
- tags:
    version: v1
  weight: 100
```

The destination is the name of the service (specified as a fully qualified
domain name (FQDN)) to which the traffic is being routed. In a Kubernetes
deployment of Istio, the route *tag*
"version: v1" corresponds to a Kubernetes *label* "version: v1".  The rule
ensures that only Kubernetes pods containing the label "version: v1"
will receive traffic.

There are two types of rules in Istio, **Route Rules**,
which control request routing, and **Destination Policies**,
which specify policies, for example, circuit breakers, that control requests for a destination service.

Istio rules can be set and displayed using the [istioctl CLI](istioctl.md).
For example, the above rule can be set using the following command:
```bash
$ cat <<EOF | istioctl create
type: route-rule
name: reviews-default
spec:
  destination: reviews.default.svc.cluster.local
  route:
  - tags:
      version: v1
    weight: 100
EOF
```

## Route Rules

The following subsections provide a overview of the basic fields of a route rule.
A complete and detailed description of a route rule structure can be found
[here](https://github.com/istio/api/blob/master/proxy/v1/config/cfg.md#route-rules).

### destination

Every rule corresponds to some destination microservice identified by a *destination* field in the
rule. For example, all rules that apply to calls to the "reviews" microservice will include the following field.

```yaml
destination: reviews.default.svc.cluster.local
```

The *destination* value SHOULD be a fully qualified domain name (FQDN). It
is used by the Istio runtime for matching rules to services. For example,
in Kubernetes, a fully qualified domain name for a service can be
constructed using the following format: *serviceName.namespace.dnsSuffix*. 

### precedence

The order of evaluation of rules corresponding to a given destination, when there is more than one, can be specified 
by setting the *precedence* field of the rule. 

```yaml
destination: reviews.default.svc.cluster.local
precedence: 1
```

The precedence field is an optional integer value, 0 by default.
Rules with higher precedence values are evaluated first.
If there is more than one rule with the same precedence value the order of evaluation is undefined.

For further details, see [Route Rule Evaluation](#route-rule-evaluation).

### match

Rules can optionally be qualified to only apply to requests that match some specific criteria such
as a specific request source and/or headers. An optional *match* field is used for this purpose.

The *match* field is an object with the following nested fields:

* **source** fully qualified domain name (FQDN)
* **sourceTags** optional match qualifications
* **httpHeaders** HTTP header qualifications
* **tcp**
* **udp**

#### match.source

The *source* field qualifies a rule to only apply to requests from a specific caller.
For example,
the following *match* clause qualifies the rule to only apply to calls from the "reviews" microservice.

```yaml
destination: ratings.default.svc.cluster.local
match:
  source: reviews.default.svc.cluster.local
```

The *source* value, just like *destination*, is an FQDN of a service.

#### match.sourceTags

The *sourceTags* field can be used to further qualify a rule to only apply to specific instances of a
calling service.
For example,
the following *match* clause refines the previous example to qualify the rule to only 
apply to calls from version "v2" of the "reviews" microservice.

```yaml
destination: ratings.default.svc.cluster.local
match:
  source: reviews.default.svc.cluster.local
  sourceTags:
    version: v2
```

#### match.httpHeaders

The *httpHeaders* field is a set of one or more property-value pairs where each property is an HTTP header name
and the corresponding value is one of the following:

1. `exact: "value"`, where the header value must match *value* exactly
2. `prefix: "value"`, where *value* must be a prefix of the header value
3. `regex: "value"`, where *value* is a regular expression that matches the header value

For example, the following rule will only apply
to an incoming request if it includes a "Cookie" header that contains the substring "user=jason".

```yaml
destination: reviews.default.svc.cluster.local
match:
  httpHeaders:
    Cookie:
      regex: "^(.*?;)?(user=jason)(;.*)?$"
```

If more than one property-value pair is provided, then all of the corresponding headers
must match for the rule to apply.

The *source* and *httpHeaders* fields can both be set in the *match* object in which case both criteria must pass.
For example, the following rule only applies if the source of the request is "reviews:v2" 
AND the "Cookie" header containing "user=jason" is present. 

```yaml
destination: ratings.default.svc.cluster.local
match:
  source: reviews.default.svc.cluster.local
  sourceTags:
    version: v2
  httpHeaders:
    Cookie:
      regex: "^(.*?;)?(user=jason)(;.*)?$"
```

#### match.tcp

TBD

#### match.udp

TBD

### route

The *route* field identifies a set of one or more weighted backends to call when the rule is activated.
Each *route* backend is an object with the following fields:

* **tags**
* **weight**
* **destination**

Of these fields, *tags* is the only required one, the others are optional.

#### route.tags

The *tags* field is a list of instance tags to identify the target instances (e.g., version) to route requests to.
If there are multiple registered instances with the specified tag(s),
they will be routed to based on the [load balancing policy](#loadBalancing) configured for the service,
or round-robin by default.

#### route.weight

The *weight* field is an optional value between 0 and 100 that represents the percentage of requests to route
to instances associated with the corresponding backend. If not set, the *weight* is assumed to be 100.

The sum of all route weights in a rule SHOULD equal 100.
For example, the following rule will route 25% of traffic for the "reviews" service to instances with
the "v2" tag and the remaining traffic (i.e., 75%) to "v1".

```yaml
destination: reviews.default.svc.cluster.local
route:
- tags:
    version: v2
  weight: 25
- tags:
    version: v1
  weight: 75
```

#### route.destination

The *destination* field is optional and specifies the service name of the target instances. 
If not specified, it defaults to the value of the rule's *destination* field.

### httpReqTimeout

A timeout for http requests can be specified using the *httpReqTimeout* field.
By default, the timeout is 15 seconds, but this can be overridden as follows:

```yaml
destination: "ratings.default.svc.cluster.local"
route:
- tags:
    version: v1
httpReqTimeout:
  simpleTimeout:
    timeoutSeconds: 10
```

### httpReqRetries

The *httpReqRetries* field can be used to control the number retries for a given http request.
The maximum number of attempts, or as many as possible within the time period
specified by *httpReqTimeout*, can be set as follows:

```yaml
destination: "ratings.default.svc.cluster.local"
route:
- tags:
    version: v1
httpReqRetries:
  simpleRetry:
    attempts: 3
```

### httpFault

The *httpFault* field is used to specify one or more faults to inject
while forwarding http requests to the rule's corresponding request destination.
The faults injected depend on the following nested fields:

* **delay**
* **abort**

#### httpFault.delay

The *delay* field is used to delay a request by a specified amount of time. Nested fields
*percent* and one of either *fixedDelaySeconds* or *exponentialDelaySeconds* are used to specify the delay.

The *fixedDelaySeconds* field is used to indicate the amount of delay in seconds.

Alternatively, the *exponentialDelaySeconds* field can be used to specify the mean delay
for values derived according to an exponential function.

An optional *percent* field, a value between 0 and 100, can be used to only delay a certain percentage of requests.
All request are delayed by default.

The following example will introduce a 5 second delay in 10% of the requests to the "v1" version of the "reviews" microservice.

```yaml
destination: reviews.default.svc.cluster.local
route:
- tags:
    version: v1
httpFault:
  delay:
    percent: 10
    fixedDelaySeconds: 5
```

#### httpFault.abort

An *abort* field is used to prematurely abort a request, usually to simulate a failure. Nested fields
*percent* and one of *httpStatus*, *http2Error*, or *grpcStatus*, are used to specify the abort.

The optional *percent* field, a value between 0 and 100, is used to only abort a certain percentage of requests.
All requests are aborted by default.

The *httpStatus* field is used to indicate a value to return from an HTTP request, instead of forwarding the request to the destination.
Its value is an integer HTTP 2xx, 3xx, 4xx, or 5xx status code.

Similarly, to abort HTTP/2 or gRPC, the value to return 
can be specified using the *http2Error* or *grpcStatus*, respectively.

The following example will return an HTTP 400 error code for 10% of the requests to the "ratings" service "v1".

```yaml
destination: "ratings.default.svc.cluster.local"
route:
- tags:
    version: v1
httpFault:
  abort:
    percent: 10
    httpStatus: 400
```

Sometimes delays and abort faults are used together. For example, the following rule will delay
by 5 seconds all requests from the "reviews" service "v2" to the "ratings" service "v1" and 
then abort 10 percent of them:

```yaml
destination: ratings.default.svc.cluster.local
match:
  source: reviews.default.svc.cluster.local
  sourceTags:
    version: v2
route:
- tags:
    version: v1
httpFault:
  delay:
    fixedDelaySeconds: 5
  abort:
    percent: 10
    httpStatus: 400
```

## Route Rule Evaluation

Whenever the routing story for a particular microservice is purely weight based,
it can be specified in a single rule, as shown in the [route.weight](#route-weight) example.
When, on the other hand, other crieria (e.g., requests from a specific user) are being used to route traffic,
more than one rule will be needed to specify the routing.
This is where the rule *precedence* field must be set to make sure that the rules are evaluated in the right order.

A common pattern for generalized route specification is to provide one or more higher priority rules
that use the *match* field (see [match](#match)) to provide specific rules,
and then provide a single weight-based rule with no match criteria at the lowest priority to provide the
weighted distribution of traffic for all other cases.

The following 2 rules, together, specify that all requests for the "reviews" service
that includes a header named "Foo" with the value "bar" will be sent to the "v2" instances.
All remaining requests will be sent to "v1".

```yaml
destination: reviews.default.svc.cluster.local
precedence: 2
match:
  httpHeaders:
    Foo:
      exact: bar
route:
- tags:
    version: v2
---
destination: reviews.default.svc.cluster.local
precedence: 1
route:
- tags:
    version: v1
  weight: 100
```

Notice that the header-based rule has the higher precedence (2 vs. 1). If it was lower, these rules wouldn't work as expected since the
weight-based rule, with no specific match criteria, would be evaluated first which would then simply route all traffic
to "v1", even requests that include the matching "Foo" header. Once a rule is found that applies to the incoming
request, it will be executed and the rule-evaluation process will terminate. That's why it's very important to
carefully consider the priorities of each rule when there is more than one.

## Destination Policies

The following subsections provide a overview of the basic fields of a destination policy.
A complete and detailed description of a destination policy structure can be found
[here](https://github.com/istio/api/blob/master/proxy/v1/config/cfg.md#destination-policies).

### destination

Exactly the same as in a [route rule](#route-rules),
every destination policy includes a *destination* field
which identifies the destination microservice for the rule.
For example, a destination policy for calls to the "reviews" microservice will include the following field.

```yaml
destination: reviews.default.svc.cluster.local
```

### tags

An optional field, *tags*, is a list of destination tags that can be used to only apply the policy
to requests that are routed to backends with the specified tags.

The following policy will only apply to requests targetting the "v1" version of the "reviews" microserivice.

```yaml
destination: reviews.default.svc.cluster.local
tags:
  version: v1
```

### loadBalancing

The *loadBalancing* field can be used to specify the load balancing policy for a destination service.
The value can be one of `LEAST_CONN`, `RANDOM`, or `ROUND_ROBIN` (default).

The following destination policy specifies that RANDOM load balancing be used for balancing across
instances of any version of the "reviews" microservice:

```yaml
destination: reviews.default.svc.cluster.local
loadBalancing: RANDOM
```

### circuitBreaker

The *circuitBreaker* field can be used to set a circuit breaker for a particular microservice. 
A simple circuit breaker can be set based on a number of criteria such as connection and request limits.

For example, the following destination policy
sets a limit of 100 connections to "reviews" service version "v1" backends.

```yaml
destination: reviews.default.svc.cluster.local
tags:
  version: v1
circuitBreaker:
  simpleCb:
    maxConnections: 100
```

The complete set of simple circuit breaker fields can be found
[here](https://github.com/istio/api/blob/master/proxy/v1/config/cfg.md#circuitbreakersimplecircuitbreakerpolicy).

## Destination Policy Evaluation

Similar to route rules, destination policies are associated with a particular *destination* however
if they also include *tags* their activation depends on route rule evaluation results.

The first step in the rule evaluation process evaluates the route rules for a *destination*,
if any are defined, to determine the tags (i.e., specific version) of the destination service
that the current request will be routed to. Next, the set of destination policies, if any, are evaluated
to determine if they apply.

One subtlety of the algorithm to keep in mind is that policies that are defined for specific
tagged destinations will only be applied if the corresponding tagged instances are explicity
routed to. For example, consider the following rule, as the one and only rule defined for
the "reviews" microservice.

```yaml
destination: reviews.default.svc.cluster.local
tags:
  version: v1
circuitBreaker:
  simpleCb:
    maxConnections: 100
```

Since there is no specific route rule defined for the "reviews" microservice, default
round-robin routing behavior will apply, which will persumably call "v1" instances on occasion,
maybe even always if "v1" is the only running version. Nevertheless, the above policy will never
be invoked since the default routing is done at a lower level. The rule evaluation engine will be
unaware of the final destination and therefore unable to match the destination policy to the request. 

You can fix the above example in one of two ways. You can either remove the `tags:`
from the rule, if "v1" is the only instance anyway, or, better yet, define proper route rules
for the service. For example, you can add a simple route rule for "reviews:v1".

```yaml
destination: reviews.default.svc.cluster.local
route:
- tags:
    version: v1
```

Although the default Istio behavior conveniently sends traffic from all versions of a source service
to all versions of a destination service without any rules being set, 
as soon as version discrimination is desired rules are going to be needed.

Therefore, setting a default rule for every microservice, right from the start,
is generally considered a best practice in Istio.
