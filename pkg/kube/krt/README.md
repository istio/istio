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

In general, these are built by providing some `func(inputs...) outputs...` (called "transformation" functions).
While more could be expressed, there are currently three forms implemented.

* `func() *O` via `NewSingleton`
    * This generates a collection that has a single value. An example would be some global configuration.
* `func(input I) *O` via `NewCollection`
    * This generates a one-to-one mapping of input to output. An example would be a transformation from a `Pod` type to a generic `Workload` type.
* `func(input I) []O` via `NewManyCollection`
    * This generates a one-to-many mapping of input to output. An example would be a transformation from a `Service` to a _set_ of `Endpoint` types.
    * The order of the response does not matter. Each response must have a unique key.

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
The framework will use generic Equals on the underlying object to determine whether or not to recompute collections.

### Picking a collection type

There are a variety of collection types available.
Picking these is about simplicity, usability, and performance.

The `NewSingleton` form (`func() *O`), in theory, could be used universally.
Consider a transformation from `Pod` to `SimplePod`:

```go
SimplePods := krt.NewSingleton[SimplePod](func(ctx krt.HandlerContext) *[]SimplePod {
    res := []SimplePod{}
    for _, pod := range krt.Fetch(ctx, Pod) {
        res = append(res, SimplePod{Name: pod.Name})
    }
    return &res
}) // Results in a Collection[[]SimplePod]
```

While this *works*, it is inefficient and complex to write.
Consumers of SimplePod can only query the entire list at once.
Anytime *any* `Pod` changes, *all* `SimplePod`s must be recomputed.

A better approach would be to lift `Pod` into a primary dependency:

```go
SimplePods := krt.NewCollection[SimplePod](func(ctx krt.HandlerContext, pod *v1.Pod) *SimplePod {
    return &SimplePod{Name: pod.Name}
}) // Results in a Collection[SimplePod]
```

Not only is this simpler to write, its far more efficient.
Consumers can more efficiently query for `SimplePod`s using label selectors, filters, etc.
Additionally, if a single `Pod` changes we only recompute one `SimplePod`.

Above we have a one-to-one mapping of input and output.
We may have one-to-many mappings, though.
In these cases, usually its best to use a `ManyCollection`.
Like the above examples, its *possible* to express these as normal `Collection`s, but likely inefficient.

Example computing a list of all container names across all pods:

```go
ContainerNames := krt.NewManyCollection[string](func(ctx krt.HandlerContext, pod *v1.Pod) (res []string) {
    for _, c := range pod.Spec.Containers {
      res = append(res, c.Name)
    }
    return res
}) // Results in a Collection[string]
```

Example computing a list of service endpoints, similar to the Kubernetes core endpoints controller:

```go
Endpoints := krt.NewManyCollection[Endpoint](func(ctx krt.HandlerContext, svc *v1.Service) (res []Endpoint) {
    for _, c := range krt.Fetch(ctx, Pods, krt.FilterLabel(svc.Spec.Selector)) {
      res = append(res, Endpoint{Service: svc.Name, Pod: pod.Name, IP: pod.status.PodIP})
    }
    return res
}) // Results in a Collection[Endpoint]
```

As a rule of thumb, if your `Collection` type is a list, you most likely should be using a different type to flatten the list.
An exception to this would be if the list represents an atomic set of items that are never queried independently;
in these cases, however, it is probably best to wrap it in a struct.
For example, to represent the set of containers in a pod, we may make a `type PodContainers struct { Name string, Containers []string }` and have a
`Collection[PodContainers]` rather than a `Collection[[]string]`.

In theory, other forms could be expressed such as `func(input1 I1, input2 I2) *O`.
However, there haven't yet been use cases for these more complex forms.

### Transformation constraints

In order for the framework to properly handle dependencies and events, transformation functions must adhere by a few properties.

Basically, Transformations must be stateless and idempotent.
* Any querying of other `Collection`s _must_ be done through `krt.Fetch`.
* Querying other data stores that may change is not permitted.
* Querying external state (e.g. making HTTP calls) is not permitted.
* Transformations _may_ be called at any time, including many times for the same inputs. Transformation functions should not make any assumptions about calling patterns.

Violation of these properties will result in undefined behavior (which would likely manifest as stale data).

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
* `FilterGeneric(func(any) bool)`: filters by an arbitrary function.

Note that most filters may only be used if the objects being `Fetch`ed implement appropriate functions to extract the fields filtered against.
Failures to meet this requirement will result in a `panic`.

## Library Status

This library is currently "experimental" and is not used in Istio production yet.
The intent is this will be slowly rolled out to controllers that will benefit from it and are lower risk;
likely, the ambient controller will be the first target.

While its _plausible_ all of Istio could be fundamentally re-architected to fully embrace `krt` throughout (replacing things like `PushContext`),
it is not yet clear this is desired.

### Performance

Compared to a perfectly optimized hand-written controller, `krt` adds some overhead.
However, writing a perfectly optimized controller is hard, and often not done.
As a result, for many scenarios it is expected that `krt` will perform on-par or better.

This is similar to a comparison between a high level programming language compared to assembly;
while its always possible to write better code in assembly, smart compilers can make optimizations humans are unlikely to,
such as loop unrolling.
Similarly, `krt` can make complex optimizations in one place, so each controller implementation doesn't, which is likely to increase
the amount of optimizations applied.

The `BenchmarkControllers` puts this to the test, comparing an *ideal* hand-written controller to one written in `krt`.
While the numbers are likely to change over time, at the time of writing the overhead for `krt` is roughly 10%:

```text
name                  time/op
Controllers/krt-8     13.4ms ±23%
Controllers/legacy-8  11.4ms ± 6%

name                  alloc/op
Controllers/krt-8     15.2MB ± 0%
Controllers/legacy-8  12.9MB ± 0%
```

### Future work

#### Object optimizations

One important aspect of `krt` is its ability to automatically detect if objects have changed, and only trigger dependencies if so.
This works better when we only compare fields we actually use.
Today, users can do this manually by making a transformation from the full object to a subset of the object.

This could be improved by:
* Automagically detecting which subset of the object is used, and optimize this behind the scenes. This seems unrealistic, though.
* Allow a lightweight form of a `Full -> Subset` transformation, that doesn't create a full new collection (with respective overhead), but rather overlays on top of an existing one.

#### Internal dependency optimizations

Today, the library stores a mapping of `Input -> Dependencies` (`map[Key[I]][]dependency`).
Often times, there are common dependencies amongst keys.
For example, a namespace filter probably has many less unique values than unique input objects.
Other filters may be completely static and shared by all keys.

This could be improved by:
* Optimize the data structure to be a bit more advanced in sharing dependencies between keys.
* Push the problem to the user; allow them to explicitly set up static `Fetch`es.

#### Debug tooling

`krt` has an opportunity to add a lot of debugging capabilities that are hard to do elsewhere, because it would require
linking up disparate controllers, and a lot of per-controller logic.

Some debugging tooling ideas:
* Add OpenTelemetry tracing to controllers ([prototype](https://github.com/howardjohn/istio/commits/experiment/cv2-tracing)).
* Automatically generate mermaid diagrams showing system dependencies.
* Automatically detect violations of [Transformation constraints](#transformation-constraints).
