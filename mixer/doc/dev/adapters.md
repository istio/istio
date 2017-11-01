# Istio Mixer: Adapter Developer's Guide

This document is for developers looking to build an [adapter](https://istio.io/docs/concepts/policy-and-control/mixer.html#adapters)
for Istio's Mixer. Adapters integrate Mixer with different infrastructure backends that deliver core functionality, such
as logging, monitoring, quotas, ACL checking, and more. This guide explains the adapter model and adapter lifecycle, and
also walks through the step-by-step instructions for creating a simple adapter.

* [Background](#background)
* [Template overview](#template-overview)
* [Adapter lifecycle](#adapter-lifecycle)
* [Example](#example)
* [Summary diagrams](#summary-diagrams)
* [Plug adapter into Mixer](#plug-adapter-into-mixer)
* [Testing](#testing)
* [Do's and dont's](#dos-and-donts)
* [Built-in templates](#built-in-templates)
* [Walkthrough](#walkthrough-of-adapter-implementation-30-minutes)

# Background

[Mixer](https://istio.io/docs/concepts/policy-and-control/mixer.html) provides Istio's generic intermediation layer
between application code and infrastructure backends such as quota systems, access control systems, logging, and so
on. It is an [attribute](https://istio.io/docs/concepts/policy-and-control/attributes.html) processing engine that uses
operator-supplied [configuration](https://istio.io/docs/concepts/policy-and-control/mixer-config.html) to map request
attributes from the proxy into calls to these backend systems via a pluggable set of [adapters](https://istio.io/docs/concepts/policy-and-control/mixer.html#adapters).
Adapters enable Mixer to expose a single consistent API, independent of the infrastructure backends in use. The exact
set of adapters used at runtime is determined through operator configuration and can easily be extended to target new
or custom infrastructure backends.

![mixer architecture](./img/mixer%20architecture.svg)

Mixer structures its incoming attributes into a more useful form for adapters using
templates. Templates describe the form of data dispatched to adapters when processing a request and the interface that
the adapter must implement to receive this data. The operator that configures Istio controls how
this template-specific data is constructed and dispatched to adapters.

Out of the box, Mixer provides a suite of [default templates](#built-in-templates).
We strongly recommend that, when implementing adapters, you use Mixer's
default templates. Though if they are not suitable for your particular needs you can also [create your own templates](./templates.md)
along with adapters to consume the relevant data. Mixer also includes some [built-in adapters](https://github.com/istio/mixer/tree/master/adapter)
by default, but users may need to implement their own to let Mixer send data to their chosen backend.

The roles of the template author, adapter author, and the operator can be summarized as:

* The template author defines a *template*, which describes the data Mixer dispatches to adapters, and the
  interface that the adapter must implement to process that data. The supported set of templates within Mixer
  determines the various types of data an operator can configure Mixer to create and dispatch to the adapters.

* The adapter author selects the templates he/she wants to support based on the functionality the adapter must provide.
  The adapter author's role is to implement the required set of template-specific interfaces to process the data
  dispatched by Mixer at runtime.

* The operator defines what data should be collected
  ([instances](https://istio.io/docs/concepts/policy-and-control/mixer-config.html#instances)), where it can be sent
  ([handlers](https://istio.io/docs/concepts/policy-and-control/mixer-config.html#handlers)), and when to send it
  ([rules](https://istio.io/docs/concepts/policy-and-control/mixer-config.html#rules)).

![operator, adapter and template devs](./img/operator%20template%20adapter%20dev.svg)


# Template overview

To understand how an adapter receives and processes a template-specific instance, this section first
provides details about various artifacts of a template that are relevant for adapter development.

As we saw in the previous section, a build of Mixer supports a set of templates, and every template defines a kind
of data Mixer dispatches to adapters when processing a request, and also defines the interface for adapters to consume
that data.

The following diagram shows the various components of a template.

![template generated artifacts](./img/template%20generated%20artifacts.svg)

We'll look at each of these in more detail below.

## Template proto file

Templates are defined using a proto file with a message named `'Template'`. `Template` is a simple proto message with
no associated code. All of the Go artifacts used by adapters are automatically generated from the template protos.

Every template also has two additional properties associated with it:

* **Name**: Every template has a unique name. Adapter code uses the name of the template to register with Mixer that it
wants to consume `Instance` objects associated with a particular template. The template name is also used within
operator config to provide template-specific fields to attribute mapping, which is used to create `Instance `objects.

* **Template Variety**: Every template has a specific `template_variety` which can be either Check, Report or Quota.
The template and its variety determine the signatures of the methods that adapters must implement for consuming the
associated instances. The `template_variety` also determines under which of the core Mixer behaviors, check report or
quota, the `instances` for the templates should be created and dispatched to adapters.

## Generated Go code

Individual templates are processed in order to produce four Go artifacts:

* *Instance* struct: This defines the data that is passed to the adapters at request time. Mixer
constructs objects of the `Instance` type, based on the request attributes and operator configuration.

* *Handler* interface: This defines methods that Mixer uses to dispatch created `Instance` objects to the adapters at
request time. Adapters must implement one Handler interface per supported template type.

* *Type* struct: If the datatype of a field in the `Instance` Go struct is dynamic (`interface{}`), the datatype of the value it will
hold during request time is determined based on operator-supplied configuration. The type struct expresses
the datatype of dynamic fields using the [ValueType enum](https://github.com/istio/api/blob/master/mixer/v1/config/descriptor/value_type.proto),
which has 1:1 mapping between Go data types and its enum values.

* *HandlerBuilder* interface: This defines the methods that Mixer uses to pass the `Types`
to the adapter.  Mixer passes all possible type information for which the adapter might expect to receive corresponding
`Instance` objects at request time. Adapters implement per template `HandlerBuilder` interface for Mixer to call into them.

## Examples

These examples show three templates, one for each of the possible `template_variety` types. Each example shows a
`Template` message, and the resulting generated Go code.

### REPORT variety template

Template.proto

```proto

syntax = "proto3";

package metric;

import "mixer/v1/config/descriptor/value_type.proto";
import "pkg/adapter/template/TemplateExtensions.proto";

option (istio.mixer.v1.config.template.template_variety) = TEMPLATE_VARIETY_REPORT;

// Metric represents a single piece of data to report.
message Template {
   // The value being reported.
   istio.mixer.v1.config.descriptor.ValueType value = 1;

   // The unique identity of the particular metric to report.
   map<string, istio.mixer.v1.config.descriptor.ValueType> dimensions = 2;
}
```

Auto-generated Go code used by adapter implementation

```golang

package metric

import (
  "context"
  "istio.io/istio/mixer/pkg/adapter"
)

const TemplateName = "metric"

type Instance struct {
  Name string
  Value interface{}
  Dimensions map[string]interface{}
}

type Type struct {
  Value      istio_mixer_v1_config_descriptor.ValueType
  Dimensions map[string]istio_mixer_v1_config_descriptor.ValueType
}

type Handler interface {
  adapter.Handler
  HandleMetric(context.Context, []*Instance) error
}

type HandlerBuilder interface {
  adapter.HandlerBuilder
  SetMetricTypes(map[string]*Type /*Instance name -> Type*/)
}
```


### CHECK  variety template

Template.proto

```proto

syntax = "proto3";

package listentry;

import "pkg/adapter/template/TemplateExtensions.proto";

option (istio.mixer.v1.config.template.template_variety) = TEMPLATE_VARIETY_CHECK;

// ListEntry is used to verify the presence/absence of a string
// within a list.
message Template {
    // Specifies the entry to verify in the list.
    string value = 1;
}
```

Auto-generated Go code used by adapter implementation

```golang

package listentry

import (
	"context"

	"istio.io/istio/mixer/pkg/adapter"
)

const TemplateName = "listentry"

type Instance struct {
	// Name of the instance as specified in configuration.
	Name string

	// Specifies the entry to verify in the list.
	Value string
}

type Type struct {
}

type HandlerBuilder interface {
	adapter.HandlerBuilder
	SetListEntryTypes(map[string]*Type /*Instance name -> Type*/)
}

type Handler interface {
	adapter.Handler
	HandleListEntry(context.Context, *Instance) (adapter.CheckResult, error)
}
```


### QUOTA variety template

Template.proto

```proto

syntax = "proto3";

package metric;

import "mixer/v1/config/descriptor/value_type.proto";
import "pkg/adapter/template/TemplateExtensions.proto";

option (istio.mixer.v1.config.template.template_variety) = TEMPLATE_VARIETY_REPORT;

// Metric represents a single piece of data to report.
message Template {
   // The value being reported.
   istio.mixer.v1.config.descriptor.ValueType value = 1;

   // The unique identity of the particular metric to report.
   map<string, istio.mixer.v1.config.descriptor.ValueType> dimensions = 2;
}
```

Auto-generated Go code used by adapter implementation


```golang

package quota

import (
	"context"

	"istio.io/istio/mixer/pkg/adapter"
)

const TemplateName = "quota"

type Instance struct {
	// Name of the instance as specified in configuration.
	Name string

	// The unique identity of the particular quota to manipulate.
	Dimensions map[string]interface{}
}

type Type struct {
	Dimensions map[string]istio_mixer_v1_config_descriptor.ValueType
}

type HandlerBuilder interface {
	adapter.HandlerBuilder
	SetQuotaTypes(map[string]*Type /*Instance name -> Type*/)
}

type Handler interface {
	adapter.Handler
	HandleQuota(context.Context, *Instance, adapter.QuotaArgs) (adapter.QuotaResult, error)
}
```


# Adapter lifecycle

This section explains various Mixer states during which it interacts with adapters. This is important in order to
understand how to implement adapter code and manage the states of various objects within the adapter code itself, such
as remote connections and local caches. This section explains how an adapter implementation is usually structured to
achieve clear separation of concerns between configuration-time and request-time responsibilities.

## Common adapter code layout

Every adapter must implement :

* A Go struct that implements '`HandlerBuilder'`interfaces for all supported templates.

* A Go struct that implements '`Handler' `interfaces for all supported templates.

An adapter implementation therefore usually contains a Go struct named `builder` and a Go struct named `handler`.
(The name of the structs is not important, but for the purpose of this document, let's call them `builder` and `handler`).


## Mixer-adapter interactions

Mixer has three states during which it interacts with adapters: initialization-time, configuration-time and request-time.

High level flow between Mixer and adapters.

![flow: mixer and adapter interaction](./img/mixer%20adapter%20flow.svg)

Let's take a detailed look at them:

### Initialization-time

This is when Mixer is booted and adapters are initialized. Every adapter must implement a `GetInfo `function which
returns an `adapter.Info` object. During initialization, Mixer invokes this function for all known adapters. The
`adapter.Info` describes the templates an adapter wants to support as well as how to construct its `builder` object.

`adapter.Info` (Details in [info.go](https://github.com/istio/mixer/blob/master/pkg/adapter/info.go)) contains the
following information:


```golang
type Info struct {
	// Name returns the official name of the adapter, it must be RFC 1035 compatible DNS
	// label.
	// Regex: "^[a-z]([-a-z0-9]*[a-z0-9])?$"
	// Name is used in Istio configuration, therefore it should be descriptive but short.
	// example: denier
	// Vendor adapters should use a vendor prefix.
	// example: mycompany-denier
	Name string
	
	// Impl is the package implementing the adapter.
	// example: "istio.io/istio/mixer/adapter/denier"
	Impl string
	
	// Description returns a user-friendly description of the adapter.
	Description string
	
	// NewBuilder is a function that creates a Builder which implements Builders
	// associated with the SupportedTemplates.
	NewBuilder NewBuilderFn
	
	// SupportedTemplates expresses all the templates the adapter wants to serve.
	SupportedTemplates []string
	
	// DefaultConfig is a default configuration struct for this
	// adapter. This will be used by the configuration system to establish
	// the shape of the block of configuration state passed to the HandlerBuilder.Build
	// method.
	DefaultConfig proto.Message
}
```


### Configuration-time

This is when the operator configuration is loaded/reloaded. During configuration, Mixer creates new `builder` objects,
configures them, and instantiates `handler` objects for the adapter.

Details about the configuration time Mixer-Adapter interaction:

**Creating new `builder`**

Every handler config block in the operator's config results in an instance of `builder` type

![handler config](./img/handler%20config.svg)

**Passing template-specific types and adapter-specific config to `builder`**

After `builder` object instantiation, Mixer configures the `builder` object by invoking various template-specific
`HandlerBuilder `interface methods (example `SetMetricTypes`, `SetQuotaTypes` for 'metric' and 'quota' named templates.)
and passing a map of string-to-`Type `struct. The string key and the value `Type `represents the name of the instance as
configured by the operator and the shape of the `Instance` object the adapter would receive at request time.

Given the above sample handler configuration and 'metric' template, the example below shows configuration-time call values.


![flow: example attr to types](./img/example%20instance%20to%20type.svg)

At request time, every `Instance` object dispatched to the adapter has a `Name` field. The adapter implementation should
use the value of the `Name` field to lookup the shape description for the `Instance` object from the map of instance
name(string)->`Type` that was passed at configuration time through the `builder` object.

Once Mixer has called into various template-specific Set.*Types methods,

Mixer calls the `SetAdapterConfig` method on the `builder`, and once done then Mixer calls the `Validate` method followed by the
`Build` method. `SetAdapterConfig` gives the builder the adapter-specific configuration, `Validate` method allows builder to
validate the operator configuration based on the provided template-specific `Types` and the adapter-specific configuration.

**Instantiating `handler`**

Once `builder` is validated, Mixer calls its `Build` method, which returns a `handler` object which Mixer invokes during
request processing. The `handler` instance constructed must implement all the `Handler` interfaces (runtime request
serving interfaces) for all the templates the adapter has registered for. If the returned handler fails to implement the
required interface for the adapter's supported templates, Mixer reports an error and doesn't serve runtime traffic to
the particular handler.

The `Build` method is where adapters must do all their bootstrapping work. For example establishing connection with backend
system, initializing cache and more, that they need to start receiving data at request time.

**Closing `handler`**

When a handler is no longer useful, Mixer calls its `Close` method. In the `Close` method an adapter is expected to release
all the allocated resources and close all remote connections to the backends if it has any.

### Request-time

During this time Mixer dispatches the `instance` objects to the adapter based on the routing rules configured by the operator.
Mixer does this by invoking the Handle* functions on the `handler` object.

Given the above example operator's config (instance, action, handler configuration) and
['metric'](#report-variety-template) template, the following example shows the request-time `Instance` objects created
for a given input set of attributes:

![example attr to instance mapping](./img/example%20attr%20to%20instance.svg)

# Example

The following sample adapters illustrate the basic skeleton of the adapter code and do not provide any
functionality. They always return success. For examples of real world adapters, see [implementation of built-in adapters
within Mixer framework](https://github.com/istio/mixer/tree/master/adapter).

* Sample adapter that supports the 'metric' template

```golang
type (
  builder struct{}
  handler struct{}
)

// ensure our types implement the requisite interfaces
var _ metric.HandlerBuilder = builder{}
var _ metric.Handler = handler{}

///////////////// Configuration Methods ///////////////

func (builder) Build(Context.Context, adapter.Env) (adapter.Handler, error) {
  return handler{}, nil
}
func (builder) SetAdapterConfig(adapter.Config)                      {}
func (builder) Validate() (*adapter.ConfigErrors)                 { return }

func (builder) SetMetricTypes(map[string]*metric.Type){
  ...
}

////////////////// Runtime Methods //////////////////////////

func (handler) HandleMetric(context.Context, []*metric.Instance) error {
  return nil
}

func (handler) Close() error { return nil }

////////////////// Bootstrap //////////////////////////
// GetInfo returns the Info for this adapter.

func GetInfo() adapter.BuilderInfo {
  return adapter.BuilderInfo{
     Name:        "istio.io/istio/mixer/adapter/noop1",
     Description: "Does nothing",
     SupportedTemplates: []string{
        metric.TemplateName,
     },
     NewBuilder: func() adapter.HandlerBuilder { return builder{} },
     DefaultConfig:        &types.Empty{},
  }
}
```
* Sample adapter that supports the 'listentry' template.

```golang
type (
  builder struct{}
  handler struct{}
)

// ensure our types implement the requisite interfaces
var _ listentry.HandlerBuilder = builder{}
var _ listentry.Handler = handler{}

///////////////// Configuration Methods ///////////////

func (builder) Build(Context.Context, adapter.Env) (adapter.Handler, error) {
  return handler{}, nil
}
func (builder) SetAdapterConfig(adapter.Config)                      {}
func (builder) Validate() (*adapter.ConfigErrors)                 { return }

func (builder) SetListEntryTypes(map[string]*listentry.Type){
  ...
}


////////////////// Runtime Methods //////////////////////////

var checkResult = adapter.CheckResult{
  Status:        rpc.Status{Code: int32(rpc.OK)},
  ValidDuration: 1000000000 * time.Second,
  ValidUseCount: 1000000000,
}

func (handler) HandleListEntry(context.Context, *listentry.Instance) (adapter.CheckResult, error) {
  return checkResult, nil
}

func (handler) Close() error { return nil }

////////////////// Bootstrap //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.BuilderInfo {
  return adapter.BuilderInfo{
     Name:        "istio.io/istio/mixer/adapter/noop2",
     Description: "Does nothing",
     SupportedTemplates: []string{
        listentry.TemplateName,
     },
     NewBuilder: func() adapter.HandlerBuilder { return builder{} },
     DefaultConfig:        &types.Empty{},
  }
}

```
* Sample adapter that supports the 'quota' template.

```golang
type (
  builder struct{}
  handler struct{}
)

// ensure our types implement the requisite interfaces
var _ quota.HandlerBuilder = builder{}
var _ quota.Handler = handler{}

///////////////// Configuration Methods ///////////////

func (builder) Build(Context.Context, adapter.Env) (adapter.Handler, error) {
  return handler{}, nil
}
func (builder) SetAdapterConfig(adapter.Config)                      {}
func (builder) Validate() (*adapter.ConfigErrors)                 { return }

func (builder) SetQuotaTypes(map[string]*quota.Type){
  ...
}

////////////////// Runtime Methods //////////////////////////

func (handler) HandleQuota(ctx context.Context, _ *quota.Instance, args adapter.QuotaRequestArgs) (adapter.QuotaResult2, error) {
  return adapter.QuotaResult2{
        ValidDuration: 1000000000 * time.Second,
        Amount:        args.QuotaAmount,
     },
     nil
}

func (handler) Close() error { return nil }

////////////////// Bootstrap //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.BuilderInfo {
  return adapter.BuilderInfo{
     Name:        "istio.io/istio/mixer/adapter/noop2",
     Description: "Does nothing",
     SupportedTemplates: []string{
        quota.TemplateName,
     },
     NewBuilder: func() adapter.HandlerBuilder { return builder{} },
     DefaultConfig:        &types.Empty{},
  }
}
```


The above section provided a complete example of a simple adapter. In the next sections we'll look in more detail
how to build Mixer with custom adapters that are not shipped with the default Mixer build, and step-by-step guide to
build a simple adapter.

# Summary diagrams

The diagrams below show the relationship between, adapters, templates, operator configuration, and Mixer. They also show the flow of
Mixer at boot time, how it interacts with adapters and operator configuration. The diagrams also demonstrate how handler,
rule and instance configuration is translated to calls into adapters at configuration-time and request time.

First let's look into how Mixer, adapters, templates and operator configurations are related

![template, adapter and operator config relationship](./img/template%2C%20adapter%20and%20operator%20config%20relationship.svg)

Now that we have understood the relationship between various artifacts, let's look into what happens at the time of Mixer
start, when operator configuration is loaded/changed and when request is received.

![flow: mixer start](./img/mixer%20start%20flow.svg)

![flow: operator config change](./img/operator%20config%20change.svg)

![flow: incoming api](./img/request%20time.svg)

# Plug adapter into Mixer

For a new adapter to plug into Mixer, you will have to add your adapter's reference into Mixer's inventory
[build file](https://github.com/istio/mixer/blob/master/adapter/BUILD)'s inventory_library rule. In the *deps* section
add a reference to adapter's go_library build rule, and in the *packages* section add the short name and the go import
path to the adapter package that implements the `GetInfo `function. These two changes will plug your custom adapter into Mixer.

# Testing

We provide a simple adapter test framework. The framework instantiates an in-process Mixer gRPC server with a config store
backed by the local filesystem, and also a Mixer gRPC client in test process, which allows stepping through adapter code in
test cases. The test framework is implemented in the [test/testenv](../../test/testenv)
directory. A [sample](../../test/testenv/testenv_test.go) test is provided to show
how to use this test framework to test a dummy adapter called denier. To setup the environment, adapter developer need
author adapter config files. Sample adapter config can be found in [/testdata](../../testdata/config) directory.

# Do's and dont's

* Adapters must use env.Logger for logging during execution. This logger understands about which adapter is running and
routes the data to the place where the operator wants to see it.

* Adapters must use env.ScheduleWork or env.ScheduleDaemon in order to dispatch goroutines. This ensures all adapter
goroutines are prevented from crashing Mixer as a whole by catching any panics they produce.

# Built-in templates

Mixer ships with a set of built-in templates that are ready to use by adapters:

* [listentry](https://github.com/istio/mixer/tree/master/template/listentry)

* [logentry](https://github.com/istio/mixer/tree/master/template/logentry)

* [quota](https://github.com/istio/mixer/tree/master/template/quota)

* [metric](https://github.com/istio/mixer/tree/master/template/metric)

* [checknothing](https://github.com/istio/mixer/tree/master/template/checknothing)

* [reportnothing](https://github.com/istio/mixer/tree/master/template/reportnothing)

Using the above templates, the Mixer team has implemented a set of adapters that ships as part of the default Mixer
binary. They are located at [istio/mixer/adapter](https://github.com/istio/mixer/tree/master/adapter). They are good
examples for reference when implementing new adapters.

# Walkthrough of adapter implementation (30 minutes)

Please refer to [Adapter Development Walkthrough](./adapter-development-walkthrough.md)

