---
title: Mixer Configuration
headline: Configuring the mixer
sidenav: doc-side-concepts-nav.html
bodyclass: docs
layout: docs
type: markdown
---

{% capture overview %}

This page describes the Istio mixer's configuration model.
 
{% endcapture %}

{% capture body %}

## Background

Istio is a sophisticated system with hundreds of independent features. An Istio deployment can be a sprawling
affair potentially involving dozens of microservices, with a swarm of Istio proxy and mixer instances to
support them. In large deployments, many different operators, each with different scope and areas of responsibility,
may be involved in managing the overall deployment.

The Istio configuration model makes it possible to exploit all of Istio's capabilities and flexibility, while
remaining relatively simple to use. The model's scoping and inheritance features enable large
support organizations to collectively manage complex deployments with ease. Some of the model's key
features include:

- **Designed for Operators**. Service operators control all operational and policy
aspects of an Istio deployment by manipulating configuration records.

- **Scoped**. Configuration is described hierarchically, enabling both coarse global control as well
as fine-grained local control.

- **Flexible**. The configuration model is built around Istio's [attributes]({{site.baseurl}}/docs/concepts/attributes.html),
enabling operators unprecedented control over the policies used and telemetry produced within a deployment.

- **Robust**. The configuration model is designed to provide maximum static correctness guarantees to help reduce
the potential for bad configuration changes leading to service outages.

- **Extensible**. The model is designed to support Istio's overall extensibility story. New or custom
[adapters]({{site.baseurl}}/docs/concepts/mixer.html#adapters)
can be added to Istio and be fully manipulated using the same general mechanisms as any other adapter.

## Concepts

Istio configuration is expressed using a YAML format. It is built on top of five core
abstractions:

|Concept                                       |Description
|----------------------------------------------|-----------
|[Adapters](#adapters)                         | Low-level operationally-oriented configuration state for individual mixer adapters.
|[Descriptors](#descriptors)                   | Type definitions which describe the shape of the policy objects used as input to configured aspects.
|[Manifests](#manifests)                       | Describes the attributes available in the deployment
|[Aspects](#aspects)                           | High-level intent-based configuration state describing what adapters do.
|[Scopes and Selectors](#scopes-and-selectors) | Mechanism to select which aspects, descriptors, and rules to use based on ambient attributes.

The following sections explain these concepts in detail.

### Adapters

[Adapters]({{site.baseurl}}/docs/concepts/mixer.html#adapters) are the foundational work horses that the Istio mixer is built around. Adapters
encapsulate the logic necessary to interface the mixer with specific external backend systems such as Prometheus or NewRelic. Individual adapters
generally need to be provided some basic operational parameters in order to do their work. For example, a logging adapter may need
to know the IP address and port where it's log data should be pumped.

Here's an example showing how to configure an adapter:

```
adapters:
  - name: myListChecker     # user-defined name for this block of config
    kind: lists             # kind of aspect this adapter can be used with
    impl: ipListChecker     # name of the particular adapter component to use
    params:
      publisher_url: https://mylistserver:912
      refresh_interval:
        seconds: 60
```

The `name` field gives a name to the chunk of adapter configuration so it can be referenced from elsewhere. The
`kind` field indicates the aspect kind that this configuration applies to (kinds are discussed [below](#adapters).
The `impl` field gives the name of the adapter being configured. Finally, the `params` section is where the
actual adapter-specific configuration parameters are specified. In this case, this is configuring the URL the 
adapter should use in its queries and defines the interval at which it should refresh its local caches.

For each available adapter, you can define any number of blocks of independent configuration state. This allows the same adapter
to be used multiple times within a single deployment. Depending on the situation, such as which microservice is involved, one
block of configuration will be used versus another. For example, here's another block of configuration that can coexist
with the previous one:

```
adapters:
  - name: mySecondaryListChecker
    kind: lists
    impl: ipListChecker
    params:
      publisherUrl: https://mySecondarylistserver:912
      refreshInterval:
        seconds: 5    
```
Here's another example adapter block:

```
adapters:
  - name: myMetricsCollector
    kind: metrics
    impl: prometheus
```

This configures an adapter that reports data to the Prometheus system. This adapter doesn't
require any custom parameters and so doesn't have a `params` stanza.

Each adapter defines its own particular format of configuration data. The exhaustive set of
adapter configuration formats can be found in *TBD*.

### Descriptors

Descriptors let you control the set of 
parameters which will be given to an adapter when a request is processed by the mixer.
There are different types of descriptors, each associated with particular aspect kinds:

|Descriptor Type   |Aspect Kind     |Description
|------------------|----------------|-----------
|MetricDescriptor  |metrics         |Describes what an individual metric looks like. 
|LogEntryDescriptor|application-logs|Describes what an individual log entry looks like.
|QuotaDescriptor   |quotas          |Describes what an individual quota looks like.

Descriptors are used to prepare Istio and downstream systems to accept chunks of data that
look a particular way. For example, declaring a set of metric descriptors tells Istio
the type of data different metrics will carry and the set of labels used to identity different
instances of the metric.

Here's an example metric descriptor:

```
metrics:
  - name: request_count
    kind: COUNTER
    value: INT64
    display_name: "Request Count"
    description: Request count by source, target, service, and code
    labels:
      source: STRING
      target: STRING
      service: STRING
      method: STRING
      response_code: INT64
```

The above is declaring that the system can produce metrics called `request_count`.
Such metrics will hold 64-bit integer values and be managed as absolute counters. Each
metric reported will have five labels, two specifying the source and
target names, one being the service name, one being the name of an API method and
the other being the response code for the request. Given this descriptor, Istio
can ensure that generated metrics are always properly formed, can arrange for efficient
storage for these metrics, and can ensure backend systems are ready to accept
these metrics. The `display_name` and `description` fields are optional and 
are communicated to backend systems which can use the text to enhance their
metric visualization interfaces.

Objects that are produced relative to a particular descriptor are
referred generally as *policy objects*. Examples of policy objects 
include metrics and log entries.

The different descriptor types are detailed in *TBD*

### Manifests

Manifests are used to capture invariants about the components involved in a particular Istio deployment. The only
kind of manifest supported at the moment are *attribute manifests* which are used to define the exact
set of attributes produced by individual components. Manifests are supplied by component producers
and inserted into a deployment's configuration.

Here's part of the manifest for the Istio proxy:

```
manifests:
  - name: istio-proxy
    revision: "1"
    attributes:
      source.name:
        value_type: STRING
        description: The name of the source.
      target.name:
        value_type: STRING
        description: The name of the target
      source.ip:
        value_type: IP_ADDRESS
        description: Did you know that descriptions are optional?
      origin.user:
        value_type: STRING
      request.time:
        value_type: TIMESTAMP
      request.method:
        value_type: STRING
      response.code:
        value_type: INT64
      api.method:
        value_type: STRING
      api.name:
        value_type: STRING
```

### Aspects

Aspects define high-level configuration state (what is sometimes called intent-based configuration),
independent of the particular implementation details of a specific adapter type. Whereas adapters focus
on *how* to do something, aspects focus on *what* to do.

Let's look at the definition of an aspect:

```
aspects:
- kind: lists               # the aspect's kind
  adapter: myListChecker    # the adapter to use to implement this aspect 
  params:
    blacklist: true
    check_expression: source.ip
```

The `kind` field distinguishes the behavior of the aspect being defined. The supported kinds
of aspects are shown in the following table.

|Kind             |Description
|-----------------|-----------
|access-logs      |Produces fixed-format access logs for every request.
|application-logs |Produces flexible application logs for every request.
|attributes       |Produces supplementary attributes for every request.
|denials          |Systematically produces a predictable error code.
|lists            |Verifies a symbol against a list.
|metrics          |Produces a metric that measures some runtime property.
|quotas           |Tracks a quota value.

In this case, the aspect declaration specifies the `lists` kind which indicates
we're configuring an aspect whose purpose is to enable the use of whitelists or
blacklists as a simple form of access control.

The `adapter` field indicates the block of adapter configuration to associate with this
aspect. Aspects are always associated with specific adapters in this way, since an
adapter is responsible for actually carrying out the work represented by an aspect's
configuration. In this particular case, the specific adapter chosen determines the
list to use in order to perform the aspect's list checking function.

The `params` stanza is where you enter kind-specific configuration parameters. In
the case of the `lists` kind, the configuration parameters specify whether the list
is a blacklist (entries on the list lead to denials) as opposed to a whitelist
(entries not on the list lead to denials). The `check_expression` field indicates the
attribute that carries the symbol to check against the associated adapter's list.

Here's another metric aspect:

```
  - kind: metrics
    adapter: myMetricsCollector
    params:
      metrics:
      - descriptor_name: request_count
        value: "1"
        labels:
          source: source.name
          target: target.name
          service: api.name
          method: api.method
          response_code: response.code
```

This defines an aspect that produces metrics which are sent to the myMetricsCollector adapter,
which was defined previously. The `descriptor` field specifies the name of the
metric descriptor which defines the kind of metric to report. The `value` field
provides the actual value of the metric and the labels indicate which attributes
supply the label value required by the descriptor.

Each aspect kind defines its own particular format of configuration data. The exhaustive set of
aspect configuration formats can be found in *TBD*.
    
#### Attribute Expressions

The mixer features a number of independent [request processing phases]({{site.baseurl}}/docs/concepts/mixer#request-phases).
The *Attribute Processing* phase is responsible for ingesting a set of attributes and producing policy objects that are then
delivered to individual adapters. *Attribute expressions* are used to evaluate attributes
and produce values to compose policy objects.

We've already seen a few simple attribute expressions in the samples above:

```
  source: source.name
  target: target.name
  service: api.name
  method: api.method
  response_code: response.code
```

The sequences on the right-hand side of the colons are the simplest forms of attribute expressions.
They simply consist of attribute names. In the above, the `source` label will be assigned the value
of the `source.name` attribute.

The attributes that can be used in attribute expressions must be defined in an 
attribute manifest for the deployment. Within the manifest, each attribute has
a type which represents the kind of data that this attribute carries. In the
same way, attribute expressions are also typed, and their type is derived from
the attributes in the expression and the operators applied to these attributes.

The type of an attribute expression is used to ensure consistency in which attributes
are used in what situation. For example, if the metric descriptor specifies that
a particular label is of type INT64, then only attribute expressions that produce a
64-bit integers can be used to fill-in that label. This is the case for the `response_code`
label above.

Attribute expressions include the following features:

1. Check variables for equality against constants
2. Check string variables for wildcard matches
3. Logical AND/OR/NOT operators
4. Grouping semantics
5. String Concatenation
6. Substring
7. Comparison (<, <=, ==, >=, >)

Refer to *TBD* for the full attribute expression syntax.

### Scopes and Selectors

*TBD*

## Configuration API

*TBD*

## Configuration CLI

*TBD*

{% endcapture %}

{% include templates/concept.md %}
