# `krt`: **K**ubernetes Declarative Controller **R**un**t**ime

`krt` provides a framework for building _declarative_ controllers.
See the [design doc](https://docs.google.com/document/d/1-ywpCnOfubqg7WAXSPf4YgbaFDBEU9HIqMWcxZLhzwE/edit#heading=h.ffjmk8byb9gt) and [KubeCon talk](https://sched.co/1R2oY) for more background.

The framework aims to solve a few problems with writing controllers:
* Operate on any types, from any source. Kubernetes provides informers, but these only work on Kubernetes types read from Kubernetes objects.
  * `krt` can accept any object type, from any source, and handle them the same.
* Provide high level abstractions.
  * Controller authors can write simple transformation functions from `Input` -> `Output` (with dependencies); the framework handles all the state automatically.

## Key Primitives

The most important primitive provided is the `Collection` interface.
This is basically an `Informer`, but not tied to Kubernetes.

Currently, there are three ways to build a `Collection`:
* Built from an `Informer` with `WrapClient` or `NewInformer`.
* Statically configured with `NewStatic`.
* Derived from other collections (more information on this below).

Unlike `Informers`, these primitives work on arbitrary objects.
However, these objects are expected to have some properties, depending on their usage.
These are *not* expressed as generic constraints due to limitations in Go's type system.

* Each object `T` must have a unique `Key[T]` (which is just a typed wrapper around `string`) that uniquely identifies the object.
    Default implementations exist for Kubernetes objects, Istio `config.Config` objects, and `ResourceName() string` implementations.
* `Equals(k K) bool` may be implemented to provide custom implementations to compare objects. Comparison is done to detect if changes were made.
  Default implementations are available for Kubernetes and protobuf objects, and will fallback to `reflect.DeepEqual`.
* A `Name`, `Namespace`, `Labels`, and `LabelSelector` may optionally be included, for use with filters (see below).

## Derived Collections

The core of the framework is in the ability to derive collections from others.

In general, these are built by providing some `func(inputs...) outputs...`.
While more could be expressed, there are currently three forms implemented.

* `func() *O` via `NewSingleton`
  * This generates a collection that has a single value. An example would be some global configuration.
* `func(input I) *O` via `NewCollection`
  * This generates a one-to-one mapping of input to output. An example would be a transformation from a `Pod` type to a generic `Workload` type.
* `func(input I) []O` via `NewManyCollection`
  * This generates a one-to-many mapping of input to output. An example would be a transformation from a `Service` to a _set_ of `Endpoint` types.

The form used and input type only represent the _primary dependencies_, indicating the cardinality.
Each transformation can additionally include an arbitrary number of dependencies, fetching data from other collections.

For example, a simple `Singleton` example that keeps track of the number of `ConfigMap`s in the cluster:

```go
ConfigMapCount := krt.NewSingleton[int](func(ctx krt.HandlerContext) *int {
    cms := krt.Fetch(ctx, ConfigMaps)
    return ptr.Of(len(cms))
})
```

The `Fetch` operation enables querying against other collections.
If the result of the `Fetch` operation changes, the collection will automatically be recomputed; the framework handles the state and event detection.
In the above example, the provided function will be called (at least) every time there is a change to a configmap.
The `ConfigMapCount` collection will produce events only when the count changes.
The framework will automatically suppress events when nothing has changed.

### Transformation constraints

In order for the framework to properly handle dependencies and events, transformation functions must adhere by a few properties.

Basically, Transformations must be stateless.
* Any querying of other `Collection`s _must_ be done through `krt.Fetch`.
* Querying other data stores that may change is not permitted.
* Querying external state (e.g. making HTTP calls) is not permitted.
* Transformations _may_ be called at any time, including many times for the same inputs. Transformation functions should not make any assumptions about calling patterns.

### Fetch details

In addition to simply fetching _all_ resources from a collection, a filter can be provided.
This is more efficient than filtering outside of `Fetch`, as the framework can filter un-matched objects earlier, skipping redundant work.
The following filters are provided

* `FilterName(name, namespace)`: filters an object by Name and Namespace.
* `FilterNamespace(namespace)`: filters an object by Namespace.
* `FilterKey(key)`: filters an object by key.
* `FilterLabel(labels)`: filters to only objects that match these labels.
* `FilterSelects(labels)`: filters to only objects that **select** these labels. An empty selector matches everything.
* `FilterSelectsNonEmpty(labels)`: filters to only objects that **select** these labels. An empty selector matches nothing.
* `FilterGeneric(func)`: filters by an arbitrary function.

Note that most filters may only be used if the objects being `Fetch`ed implement appropriate functions to extract the fields filtered against.
