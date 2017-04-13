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

|Concept                     |Description
|----------------------------|-----------
|[Adapters](#adapters)       | Low-level operationally-oriented configuration state for individual mixer adapters.
|[Aspects](#aspects)         | High-level intent-based configuration state describing what adapters do.
|[Descriptors](#descriptors) | Type definitions which describe the shape of the policy objects used as input to configured aspects.
|[Rules](#rules)             | Controls the creation of policy objects, based on ambient attributes.
|[Selectors](#selectors)     | Mechanism to select which aspects, descriptors, and rules to use based on ambient attributes.

The following sections explain these concept in detail.

### Adapters

[Adapters]({{site.baseurl}}/docs/concepts/mixer.html#adapters) are the foundational work horses that the Istio mixer is built around. Adapters
encapsulate the logic necessary to interface the mixer with specific external backend systems such as Prometheus or NewRelic. Individual adapters
generally need to be provided some basic operational parameters in order to do their work. For example, a logging adapter may need
to know the IP address and port where it's log data should be pumped.

Here's an example showing how to configure an adapter:

```
adapters:
  - name: myChecker
    kind: lists
    impl: ipListChecker
    params:
      publisherUrl: https://mylistserver:912
      refreshInterval:
        seconds: 60    
```

The `name` field gives a name to this chunk of adapter configuration so it can be referenced from elsewhere. The
`kind` field indicates the aspect kind that this configuration applies to (kinds are discussed in the next section).
The `impl` field gives the name of the adapter being configured. Finally, the `params` section is where the
actual adapter-specific configuration parameters are specified. In this case, this is configuring the URL the 
adapter should use in its queries and defines the interval at which it should refresh its local caches.

For each available adapter, you can define any number of blocks of independent configuration state. This allows the same adapter
to be used multiple times within a single deployment. Depending on the situation, such as which microservice is involved, one
block of configuration will be used versus another.

Each adapter defines its own particular format of configuration data. The exhaustive set of
adapter configuration formats can be found in *TBD*.

### Aspects

Aspects define high-level configuration state (what is sometimes called intent-based configuration),
independent of the particular implementation details of a specific adapter type. Whereas adapters focus
on *how* to do something, aspects focus on *what* to do.

Let's look at the definition of an example aspect:

```
aspects:
- kind: lists
  adapter: myChecker
  params:
    blacklist: true
    check_expression: source.ip
```

The `kind` field distinguishes the behavior of the aspect being defined. The supported kinds
of aspects are:

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
the case of the lists kind, the configuration parameters specify whether the list
is a blacklist (entries on the list lead to denials) as opposed to a whitelist
(entries not on the list lead to denials). The check_expression field holds an
*attribute expression* which determines which attribute is evaluated in order to
produce the symbol that will be checked in the aspect's list. We'll discuss 
attribute expressions below.

Each aspect kind defines its own particular format of configuration data. The exhaustive set of
aspect configuration formats can be found in *TBD*.
    
### Descriptors

### Rules

### Selectors

A selector is a boolean expression that evaluates a set of attributes to produce a boolean result.

    fn (attributes) → boolean 

It may use any supported attributes, such as **request****.****size**, **source****.****user**. The authoritative list of attributes will be available in the Istio repo, [https://github.com/istio/api](https://github.com/istio).

## Putting it together

Since all aspect configs are defined as protos, they are strongly typed. There is a requirement to express a configuration containing multiple protos as a single yaml file.

There are several approaches to include generic protobufs in a single containing structure.  Even though google.**protobuf**.**Any **can be used to achieve polymorphism, the contained protobuf is pre serialized. This makes it difficult to edit the whole config as a single yaml document.

The other approaches are 

1. Use google.protobuf.Struct to achieve polymorphism. Even though it is weakly typed in the top level document, the underlying protos **are** strongly typed and one can be easily be converted to the other. 

2. Use Oneof {} to enumerate the list of allowable protos. 

In both those cases we can generate documentation and tools for the user so that option 1 and 2 are identical from a user’s perspective. 

Now we introduce container structs to hold the aspect specific configuration information.

( [Proto definition](https://gist.github.com/mandarjog/c223238b557ab08509c72325032e9e4b) )

Aspect is defined as follows :-  

message AspectConfig {

  string kind = 1; // a known 'kind' like metrics, logging, ipwhitelist, accesscontrol etc

  string adapter = 2; // optional, allows specifying a specific adapter implementing the kind

  google.protobuf.Struct params = 3; // parameters define the behavior of the aspect. It is used during construction of the aspect

  map<string, string> inputmap = 4; // maps available facts into the input space of the aspect. This is used while calling the aspect

}

Consider an access check scenario where we would like to check incoming requests against an IP whitelist.

kind: whitelist

params:

    Whitelist: ["9.0.0.0/24", "9.1.0.0/24"]

inputmap:

     source: **source****.****ip**

Params and inputs express the intent of the whitelist aspect. The whitelist is provided as a list of subnets in the params section. The shape of the Params struct is mandated by the whitelist aspect. It is **not **defined by the actual implementation of the whitelist. The ‘Inputs’ section maps available attributes to the input of the whitelist aspect. In the above case the whitelist aspect is expected to take one argument called "source", and we are passing "source.ip" as the source to use for the whitelist.

Next we configure details about implementation(s) that provide the ‘whitelist’ aspect, using an Adapter Config: Adapter config defines how intent based aspects are realized. 

message AdapterConfig {

  string name = 1; // Optional, allows a specific adapter configuration to be named for

                   // later use by aspects (e.g. if there is more than one adapter for a kind)

  string kind = 2; // matches the 'kind' in the 'Aspect'

  string impl = 3; // Name of the implementation of the adapter.

  google.protobuf.Struct args = 4;  // args necessary to configure the impl.

                                    // specified by the impl

// Q? Should we add batching caching and other impl params here?

}

We enable "istio.io/adapters/whitelist" to serve as the implementation of the “whitelist” aspect. 

Kind: whitelist

Name: simplewhitelist

Impl: "istio.io/mixer/adapters/whitelist"

This particular adapter does not take any extra "args" during construction so “args” is left out. 

The separation between Aspect.params and Adapter.args delineate intent based arguments from implementation specific arguments. 

Whitelist aspect defines input struct as follows

Whitelist_input_struct struct {

  Source string

}

Note that Adapters are generally configured at the istio deployment  level, and Aspects configured per service or client.

## Aspect specific configurations

This document alludes to monitoring, ratelimiter and whitelist aspects and their configuration details. While the configurations used here are correct in spirit, they are not authoritatively defined here. The important parts are how any aspect specific configuration interacts with mappers and how different aspect are composed in the single configuration.

This type of configuration enables us to add new aspects easily while every aspect defines its owns schema and semantics. 

The authoritative schema for several aspects are in the mixer repo.

### Metrics example

Consider another example where we define metrics that should be collected for a particular service. The metrics params are defined by the metrics Aspect handler. 

The aspect is defined as follows:

    kind: metrics

      params:

        metrics:

        - name: response_time     # What to call this metric outbound.

          value: metric_response_time  # well defined

          metric_kind: DELTA

          labels:

          - key: src_consumer_id

          - key: target_response_status_code

          - key: target_service_name

        Inputs:

           Attr.target_service_name: target_service_name || target_service_id 

The Metrics aspect manager uses aspectConfig.params to ascertain the list of attributes that are needed while calling this aspect. If an attribute used in the params is not  well-known or predefined, it can be defined inline in the Inputs section. Note that the attributes are denoted by attr.{attribute_name} on the left hand side so as to disambiguate from mapping to the input struct. 

The adapter that implements monitoring is defined as:

Kind: metrics

Name: metrics-statsd

Impl: "istio.io/adapters/statsd"

Args:

    Host: statd.svc.cluster

    Port: 8125

The above example defines and configures istio.io/adapters/statsd to provide the metrics aspect. The implementation specific attributes are configured as "Args". The structure of “Args” is **not mandated **by the aspect; it is defined by the statsd adapter implementation.

While defining an aspect in config, usually specifying ‘kind’ is enough and preferred. The system chooses an adapter of the specified kind. However, it is possible to specify a specific adapter name, like "metrics-statsd"  if needed.

  kind: metrics

  Adapter: metrics-statsd

      params:

        metrics:

        - name: attr.response_time

        … 

### Access Log Example

# TODO

## Pseudo Code for calling an adapter

It is instructive to look at how a configuration like this could be used to realize a call to an adapter. Consider the scenario where we would like to call the metrics-statsd aspect as described above.

We have references to the following config objects.

// for impl

adapterConfig = { 

Name: metrics-statsd

Impl: "istio.io/adapters/statsd"

Args:

    Host: statd.svc.cluster

    Port: 8125

}

// for intent

aspectConfig = { 

kind: metrics

      params:

        metrics:

        - name: attr.response_time

          metric_kind: DELTA

          value_type: INT64

          labels:

          - key: attr.consumer_id

          - key: attr.response_status_code

          - key: attr.service_name

      Inputs:

           Attr.service_name: attr.service_id || attr.service_name

}

// MetricsAspect.Report API

// MetricsAspect.report(* input_struct, map <string, ElementaryTypes> attributeMap)

Adapter = MetricsRegistry[adapterConfig.Impl].New (adapterConfig.Args)

Aspect = Adapter.New(aspectConfig.Params)

// get a list of attributes from aspectConfig.Params and return

// a map with attributes and values

attributeMap = MetricsAspectMgr.extractAttributes(aspectConfig.Params)

MetricsAspectMgr.augmentAttributeMap(attributeMap, aspectConfig.Inputs)

Input_struct = (Metrics_InputStruct*) MetricsAspectMgr.MapInputStruct(aspectConfig.Inputs)

Aspect.report (Input_struct, attributeMap)

##  Service Config

So far we have seen how to configure an individual aspect. Now we introduce AspectRule. Aspect rule is a tree structure of aspects that apply if the selector at that node evaluates to true.

message AspectRule {

  string selector = 1;  // expression based on attributes

  repeated Aspect aspects = 2;

  repeated AspectRule rules = 3;

}

// Configures a service.

message ServiceConfig {

  string subject = 1;

  string version = 2;

  repeated AspectRule rules = 3;

}

If selector evaluates to true, the nested rules are examined and the process repeats.

Example [Rules: # nested rule selected if the containing selector is true](#bookmark=id.kmp0xenaytfr)

Nested selectors are used to organize configuration in a meaningful way. Following configurations are equivalent. 

<table>
  <tr>
    <td>Selector: svc.name=="SVC1"
  .
  .
   Rules:
Selector: src.name == 'CLNT1'
Selector: src.name == 'CLNT2'</td>
    <td>Selector: svc.name=="SVC1"
  .
  .

Selector: src.name == 'CLNT1' && svc.name=="SVC1"
Selector: src.name == 'CLNT2' && svc.name=="SVC1"</td>
  </tr>
</table>


Following is a simple service config defining 1 aspect of kind "metrics".

# ServiceConfig

subject: "inventory.svc.cluster.local"

version: "2022"

Rules:

* Selector: service.name == "inventory.svc.cluster.local"

       Aspects: 

           - kind: metrics

             params:

        metrics:

        - name: attr.response_time

          metric_kind: DELTA

          value_type: INT64

          labels:

          - key: attr.consumer_id

          - key: attr.response_status_code

          - key: attr.service_name

             Inputs:

 Attr.service_name: attr.service_id || attr.service_name

‘Selector’ expression may use any attributes with wildcards. What follows is an example of using wildcards.

# ServiceConfig

**subject****:**** ****"****namespace:ns1****"**

version: "2022"

Rules:

* **Selector: service.name == "*****"**

       Aspects: 

           - kind: metrics

            …

This configuration defines the same metrics as above but for all services within the namespace. 

## Cluster Config

This config relies on a cluster config that defines an adapter of kind: metrics as shown above.

message ClusterConfig {

  string version = 1;

  repeated Adapter adapters = 2;

}

Cluster config defines an array of adapter structs. 

## Client Config

Similar to Service config, a client can also define its configuration. Its structure is identical to Service config at present. 

message ClientConfig {

  string subject = 1;

  string version = 2;

  repeated AspectRule rules = 3;

}

## Meta Adapters:

There are several adapters that are configured in the cluster config that do not pertain to any specific control plane or dataplane capability. These adapters configure the system itself.

### FactProvider Adapters

Istio looks at information it operates on in terms of facts/attributes. Facts are pieces of information that are ascertained from the request context or the environment. There is a default set of facts that the istio proxy sends to the Mixer. These are typically facts related to the request context. While running inside kubernetes, a K8sFactProvider adds facts such as K8sDeployment names and labels associated with those resources. Facts are named and typed that are used in configuration selectors, params and inputs. 

Request_size, response_status, request_url  are all well known attributes. 

### FactMapper Adapters

FactMapper adapters produce new facts from existing facts. The default fact-mapper uses a simple expression language like

Source:  source_name || source_ip

This creates (or overwrites) a fact called "source" using the other 2 known facts.

### InputMapper Adapters

Inputmapper maps from the attribute space to the specific input struct that is defined by the Aspect. Default input mapper maps from attributes to the aspect defined input_struct using inputmap.

## Overrides

At present we use a simple override rule. Configs defined with more specific "subject" override less specific ones. So a config defined with a subject of exactly one service will override aspects of the same kind that were defined with a subject as namespace or cluster. 

For example if an administrator wants to configure attr.response_time metric for all services, but define a different metric for a specific service, the following 2 configurations are needed.

# ServiceConfig

subject: "namespace:ns"

version: "2022"

Rules:

* Selector: service.name == "*"

       Aspects: 

           - kind: metrics

             params:

        metrics:

        - name: response_time_by_consumer

          value: metric_response_time

          metric_kind: DELTA

          labels:

          - key: target_consumer_id

Service inventory would like to collect a different metric. We create another ServiceConfig with subject == inventory.svc.cluster.local.

# ServiceConfig

subject: "service:inventory.svc.cluster.local"

version: "2022"

Rules:

* Selector: service.name == "inventory.svc.cluster.local"

       Aspects: 

           - kind: metrics

             params:

        metrics:

        - name: response_time_by_statuscode

   value: attr.response_time

          metric_kind: DELTA

          value_type: INT64

          labels:

         ** ****-**** key****:**** response_status_code**

This replaces aspect of kind: metric for the specified config for the Inventory service.

Client side override works in a similar way. The subject in that case is 

subject: "client:service_account:aaaa"

OR

subject: "client:shipping.svc.cluter.local"

### Expression Language

Configuration uses an expression language in 2 places.

1. Selector:   fn (attributes) → boolean 

2. Inputmap:  fn(attributes) → elementary type

### We will use an expression language that is a small subset of javascript.

The subset includes

1. Check variables for equality againsts constants

2. Check string variables for wildcard matches

3. Support  && and || and grouping

4. String Concatenation

5. Substring

If may add support for other operations as needed.

### Combining configs 

This meta aspect describes how different documents are combined and which parts can  override others parts of the document. Documents owned by "admin" can always do everything including setting up this policy.

For example cluster wide audit logging should not be overridden by individual services.

## Example

### Cluster Config

# cluster config

version: "2021"

adapters:

- kind: factProvider

  name: defaultFactProvider

  impl: "istio.io/adapter/defaultFactProvider"

- kind: factProvider

  name: k8sFactProvider

  impl: "istio.io/adapter/k8sFactProvider"

- kind: factMapper

  name: defaultFactMapper

  impl: "istio.io/adapter/defaultFactMapper"

  args:

    src_identity: src_podname || src_service_name   # Add a new global attribute

- kind: accessCheck

  name: block

  impl: "istio.io/adapter/block"

- kind: ipwhitelist

  name: ipwhitelist-url

  impl: "istio.io/adapter/ipwhitelist"

  args:

    provider_url: http://mywhitelist

- kind: ipwhitelist

  name: ipwhitelist-static

  impl: "istio.io/adapter/ipwhitelist"

  args:

    whitelist: ["10.0.0.0/24", "9.0.0.0/24"]

- kind: metrics

  name: statsd-highperf

  impl: "istio.io/adapter/statsd"

  args:

    host: statsd-fast

    port: 8125

- kind: metrics

  name: statsd

  impl: "istio.io/adapter/statsd"

  args:

    host: statsd-regular

    port: 8125

The preceding config defines adapters of several "kinds". The defaultFactMapper is used to define new attribute called src_identity. This attribute is available during rules evaluation and input mapping. 

### Service Config

# service config

subject: "namespace:ns1"

version: "1011"

rules:

- selector: target_name == "*"

  aspects:

  - kind: metrics

    params:

      metrics:   # defines metric collection across the board.

      - name: response_time_by_status_code

        value: metric.response_time     # certain attributes are metrics 

        metric_kind: DELTA

        labels:

        - key: response.status_code

  - kind: ratelimiter

    params:

      limits:  # imposes 2 limits, 100/s per source and destination

      - limit: "100/s"

        labels:

          - key: src.user_id

          - key: target.service_id

       - limit: "1000/s"  # every destination service gets 1000/s

        labels:

          - key: target.service_id

     # since target.service_id and src.service_id are

     # well known attributes no input map is needed

- selector: target_name == "Inventory.svc.cluster.local"

  aspects:

  - kind: ipwhitelist

    adapter: ipwhitelist-url    # this is defined in the cluster config

    inputmap:

      source: src.ip_addr   # inputs are used to fill the input_struct of the adapter

   rules:   # nested rule selected if the containing selector is true

* Selector: source_name == "Shipping.svc.cluster.local"

        Aspects:   

        - kind: block      # block access from shipping to inventory service.

Similar discussion applies for other aspects. Currently supported aspects are

Api, metrics, ratelimiter, quotas, accessChecks, Logging, (Loadbalacing,  …)

## Kubernetes deployment

Istio on Kubernetes can use namespace as a way to access control configuration resources. A namespace admin can create any configuration affecting the namespace as a whole or individual services running within it. A cluster admin can create configs that affect the entire cluster. With this model service config overrides are also defined by the namespace admin. 

## Using API surface for policy

The API surface for a service can be described using a Swagger spec, a gRPC spec or another protocol indicator like "mysql", “redis” or “mongo”.  A spec enables us to extract the “operation”  (operationId in Swagger or fully qualified method name in gRPC),  resource(s) being operated on and arguments from the incoming request. It is upto the proxy to extract this information if we wish to apply policies on anything more granular than the service itself. If these attributes are extracted then they are available as target.operation, target.resources and target.params.

For example a mysql protocol decoder at the proxy could extract, service=mysql.svc, operation=SELECT, resources="Table1,Table2”. At this point policy can be applied in a uniform way based on the above attributes.

## Open Issues: Deferred ?

1. Sophisticated overrides	

    1. Addition and removal of specific aspect and adapters

    2. Priority

2. Access Control to configuration resources.

3. Pre defined args and params

    3. Fully define response_time_by_statuscode in one place, and use it as a reference in others

## Routing Config

See [../reference/rule-dsl.md](../reference/rule-dsl.md).

{% endcapture %}

{% include templates/concept.md %}
