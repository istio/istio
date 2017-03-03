# Configuring Request Routing in Istio

* [Overview](#overview)
* [Routing Rules](#routing-rules)
    * [destination](#destination)
    * [precedence](#precendence)
    * [match](#match)
        * [match.source](#match-source)
        * [match.source_tags](#match-source_tags)
        * [match.http](#match-http)
        * [match.tcp](#match-tcp)
        * [match.udp](#match-udp)
    * [route](#route)
        * [route.destination](#route-destination)
        * [route.tags](#route-tags)
        * [route.weight](#route-weight)
* [Destination Policies](#destination-policies)
    * [destination](#pdestination)
    * [tags](#tags)
    * [load_balancing](#load_balancing)
    * [circuit_breaker](#circuit_breaker)
    * [http_fault](#http_fault)
        * [http_fault.delay](#http_fault_delay)
        * [http_fault.abort](#http_fault_abort)
        * [http_fault.headers](#http_fault_headers)

## Overview <a id="overview"></a>

Istio routing rules can be configured in YAML using a Rules DSL based on a proto3 schema
documented [here](https://github.com/istio/api/blob/master/proxy/v1/config/cfg.md).
For example, a simple rule to send 100% of incoming traffic for a "reviews" microservice
to version "v1" can be described using the Rules DSL as follows:

```yaml
destination: reviews.default.svc.cluster.local
route:
- tags:
    version: v1
  weight: 100
```

This is a simple example of a "routing rule".

There are two types of rules in Istio, `Routing Rules`,
which control request routing, and `Destination Policies`,
which perform actions such as fault injection in the request path.

Istio rules can be set and displayed using the [istioctl CLI]() (Documentation TBD).
For example, the above rule can be set using the following command:
```bash
$ cat <<EOF | istioctl create route-rule reviews-default -f -
destination: reviews.default.svc.cluster.local
route:
- tags:
    version: v1
  weight: 100
EOF
```
An end-to-end Istio example which sets several rules using `istioctl` can be found
[here](https://github.com/istio/istio/blob/master/demos/bookinfo.md).

## Routing Rules <a id="routing-rules"></a>

The following subsections provide a overview of the basic fields of a routing rule.
A complete and detailed description of a routing rule structure can be found
[here](https://github.com/istio/api/blob/master/proxy/v1/config/cfg.md#route-rules).

#### Property: destination <a id="destination"></a>

Every rule corresponds to some destination microservice identified by a `destination` field in the
Istio Rules DSL.
For example, all rules that apply to calls to the "reviews" microservice will include the following field.

```yaml
destination: reviews.default.svc.cluster.local
```

#### Property: precedence <a id="precedence"></a>

The order of evaluation of rules corresponding to a given destination, when there is more than one, can be specified 
by setting the `precedence` field of the rule.

```yaml
destination: reviews.default.svc.cluster.local
precedence: 1
```

The `precedence` field is an optional integer value, 0 by default.
Rules with higher priority values are executed earlier.
If there is more than one rule with the same priority value, the order of execution is undefined.

#### Property: match <a id="match"></a>

Rules can optionally be qualified to only apply to requests that match some specific criteria such
as a specific request source and/or headers. An optional `match` field is used for this purpose.

The `match` field is an object with the following nested fields:

* `source`
* `source_tags`
* `http`
* `tcp`
* `udp`

#### Property: match.source <a id="match-source"></a>

The `source` field is used to qualify a rule to only apply to requests from a specific caller.
For example,
the following `match` clause qualifies the rule to only apply to calls from the "reviews" microservice.

```yaml
destination: ratings.default.svc.cluster.local
match:
  source: reviews.default.svc.cluster.local
```

#### Property: match.source_tags <a id="match-source_tags"></a>

The `source_tags` field can be used to further qualify a rule to only apply to specific instances of a
calling service.
For example,
the following `match` clause refines the previous example to qualify the rule to only 
apply to calls from version "v2" of the "reviews" microservice.

```yaml
destination: ratings.default.svc.cluster.local
match:
  source: reviews.default.svc.cluster.local
  source_tags:
    version: v2
```

#### Property: match.http <a id="match-http"></a>

The `http` field is a set of one or more property-value pairs where each property is an HTTP header name
and the corresponding value is one of the following:
1. `exact: value`, where the header value must match `value` exactly
2. `prefix: value`, where `value` must be a prefix of the header value
3. `regex: value`, where `value` is a regular expression that matches the header value

For example, the following rule will only apply
to an incoming request if it includes a "Cookie" header that contains the substring "user=jason".

```yaml
destination: reviews.default.svc.cluster.local
match:
  http:
    Cookie:
      regex: "^(.*?;)?(user=jason)(;.*)?$"
```

If more than one property-value pair is provided, then all of the corresponding headers
must match for the rule to apply.

The `source` and `http` fields can both be set in the `match` object in which case both criteria must pass.
For example, the following rule only applies if the source of the request is "reviews:v2" 
AND the "Cookie" header containing "user=jason" is present. 

```yaml
destination: ratings.default.svc.cluster.local
match:
  source: reviews.default.svc.cluster.local
  source_tags:
    version: v2
  http:
    Cookie:
      regex: "^(.*?;)?(user=jason)(;.*)?$"
```

#### Property: match.tcp <a id="match-tcp"></a>

TBD

#### Property: match.udp <a id="match-udp"></a>

TBD

#### Property: route <a id="route"></a>

The `route` field identifies a set of one or more the weighted backends to call when the rule is activated.
Each `route` backend is an object with the following fields:

* `tags`
* `weight`
* `destination`

Of these fields, `tags` is the only required one, the others are optional.

#### Property: route.tags <a id="route-tags"></a>

The `tags` field is a list of instance tags to identify the target instances (e.g., version) to route requests to.
If there are multiple registered instances with the specified tag(s), they will be routed to in a round-robin fashion.

#### Property: route.weight <a id="route-weight"></a>

The `weight` field is an optional value between 0 and 100 that represents the percentage of requests to route
to instances associated with the corresponding backend. If not set, the `weight` is assumed to be 100.

The sum of all route weights in a rule must equal 100.
For example, the following rule will route 25% of traffic for the "reviews" service to instances with
the "v2" tag and the remaining traffic (i.e., 75%) to "v1".

```yaml
`destination: reviews.default.svc.cluster.local
 route:
 - tags:
     version: v2
   weight: 25
 - tags:
     version: v1
   weight: 75
```

#### Property: route.destination <a id="route-destination"></a>

The `destination` field is optional and specifies the service name of the target instances. 
If not specified, it defaults to the value of the rule's `destination` field.

### Routing Rule Execution

Whenever the routing story for a particular microservice is purely weight base,
it can be specified in a single rule, as in the previous example.
When, on the other hand, other crieria (e.g., requests from a specific user) are being used to route traffic,
more than one rule will be needed to specify the routing.
This is where the rule `precedence` field must be set to make sure that the rules are executed in the right order.

A common pattern for generalized route specification is to provide one or more higher priority rules
that use the `match` field (see [match](#match)) to provide specific rules,
and then provide a single weight-based rule with no match criteria at the lowest priority to provide the
weighted distribution of traffic for all other cases.

The following 2 rules, together, specify that all requests for the "reviews" service
that includes a header named "Foo" with the value "bar" will be sent to the "v2" instances.
All remaining requests will be sent to "v1".

```yaml
destination: reviews.default.svc.cluster.local
precedence: 2
match:
  http:
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

Notice that the header-based rule has the higher priority (2 vs. 1). If it was lower, these rules wouldn't work as expected since the
weight-based rule, with no specific match criteria, would be executed first which would then simply route all traffic
to "v1", even requests that include the matching "Foo" header. Once a rule is found that applies to the incoming
request, it will be executed and the rule-execution process will terminate. That's why it's very important to
carefully consider the priorities of each rule when there is more than one.

## Destination Policies <a id="destination-policies"></a>

The following subsections provide a overview of the basic fields of a destination policy.
A complete and detailed description of a destination policy structure can be found
[here](https://github.com/istio/api/blob/master/proxy/v1/config/cfg.md#destination-policies).

#### Property: destination <a id="pdestination"></a>

Exactly the same as in a [routing rule](#destination),
every destination policy includes a `destination` field
which identifies the destination microservice for the rule.
For example, a destination policy for calls to the "reviews" microservice will include the following field.

```yaml
destination: reviews.default.svc.cluster.local
```

#### Property: tags <a id="tags"></a>

Another optional field, `tags`, is a list of destination tags that can be used to only apply the policy
to requests that are routed to backends with the specified tags.

The following policy will only apply to requests targetting the "v1" version of the "reviews" microserivice.

```yaml
destination: reviews.default.svc.cluster.local
tags:
  version: v1
```

#### Property: load_balancing <a id="load_balancing"></a>

TBD

#### Property: circuit_breaker <a id="circuit_breaker"></a>

TBD

#### Property: http_fault <a id="http_fault"></a>

The `http_fault` field is used to specify one or more actions to execute
during http requests to the rule's corresponding request destination.
The actions(s) that will be executed depend on the following nested fields:

* `delay`
* `abort`
* `headers`

#### Property: http_fault.delay <a id="http_fault_delay"></a>

The delay field is used to delay a request by a specified amount of time.

#### Property: http_fault.delay.fixed_delay_seconds <a id="actions-fixed_delay_seconds"></a>

The `fixed_delay_seconds` field is used to indicate the amount of delay, in seconds.

#### Property: actions.percent <a id="actions-delay-percent"></a>

An optional `percent` field, a value between 0 and 100, can be used to only delay a certain percentage of requests.
All request are delayed by default.

The following example will introduce a 5 second delay in 10% of the requests to the "v1" version of the "reviews" microserivice.

```yaml
destination: reviews.default.svc.cluster.local
tags:
  version: v1
http_fault:
  delay:
    fixed_delay:
      percent: 10
      fixed_delay_seconds: 5
```

#### Property: http_fault.abort <a id="http_fault_abort"></a>

An abort action is used to prematurely abort a request, usually to simulate a failure.

#### Property: actions.return_code <a id="actions-abort-return_code"></a>

The `return_code` field is used to indicate a value to return from the request, instead of calling the backend.
Its value is an integer value between -5 and 599, usually an http 2xx, 3xx, 4xx, or 5xx status code.

#### Property: actions.probability <a id="actions-abort-probability"></a>

An optional `probability` field, a value between 0 and 1, can be used to only abort a certain percentage of requests.
All request are aborted by default.

#### Property: actions.tags <a id="actions-abort-tags"></a>

Another optional field, `tags`, is a list of destination tags that can be used to only abort
requests that are routed to backends with the specified tags.

The following example will return an http 400 error code for all requests from the "reviews" service "v2" to the "ratings" service "v1".

```yaml
destination: "ratings.default.svc.cluster.local"
tags:
  version: v1
http_fault:
  abort:
    percent: 100
    http_status: 400
```

```json
{
  "destination": "ratings",
  "match": {
    "source": {
      "name": "reviews",
      "tags": [ "v2" ]
    }
  },
  "actions" : [
    {
      "action": "abort",
      "tags": [ "v1" ],
      "return_code": 400
    }
  ]
}
```

### Action Rule Execution

Similar to routing rules, the destination policies are associated with a particular `destination` however
their activation often depends on routing rule results.

The first step in the rule execution process evaluates the routing rules for the `destination`,
if any are defined, to determine the tags (i.e., specific version) of the destination service
that the current request will be routed to. Next, the set of destination  rules, if any, are evaluated
according to their priorities, to find the action rule to apply.

One subtlety of the algorithm to keep in mind is that policies that are defined for specific
tagged destinations will only be applied if the corresponding tagged instances are explicity
routed to. For example, consider the following rule, as the one and only rule defined for
the "reviews" microservice.

```yaml
destination: reviews.default.svc.cluster.local
tags:
  version: v1
http_fault:
  delay:
    fixed_delay:
      percent: 10
      fixed_delay_seconds: 5
```

Since there is no specific routing rule defined for the "reviews" microservice, default
round-robin routing behavior will apply, which will persumably call "v1" instances on occasion,
maybe even alway if "v1" is the only running version. Nevertheless, the above policy will never
be invoked since the default routing is done at a lower level. The rule execution engine will be
unaware of the final destination and therefore unable to match the destination policy to the request. 

You can fix the above example in one of two ways. You can either remove the `tags:`
from the rule, if "v1" is the only instance anyway, or, better yet, define proper routing rules
for the service. For example, you can add a simple routing rule for "reviews:v1".

```yaml
destination: reviews.default.svc.cluster.local
route:
- tags:
    version: v1
```

Setting a default rule like this for every microservice is generally considered a best practice.
