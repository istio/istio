# Configuration flow in Istio Pilot

Istio configuration consists of configuration objects. Each object has kind
(for example, a routing rule), name and namespace uniquely identifying the
object.  Istio Pilot provides a REST-based interface for manipulating objects
by their keys. Objects can be listed by their kind and/or namespace, retrieved
by the object keys, deleted, created (using "POST" operation), and updated
(using "PUT" operation).

The configuration registry also provides a caching controller interface that
also allows watching for changes to the configuration store. For example, a
function can subscribe to receive object creation events for objects of a
particular kind. The controller streams the events to the subscribers, and also
maintains a local cache view of the entire store.

The flow of configuration starts at the operator (1), continues to the
platform-specific persistent store (2), and then triggers a notification event at
the remote proxy component (3).

## 1. User input

Istio Pilot provides a command line interface called `istioctl` that exposes
the REST-based configuration store interface.  The tool validates that its YAML
input satisfies the schema for the registered Istio configuration kind, and
stores the validated input in the store.

## 2. Configuration storage

Istio configuration storage is platform-specific. Below, we focus on Kubernetes,
since Istio Pilot model is close to Kubernetes model of Third-Party
Resources. In fact, `kubectl` could be used instead of `istioctl` if validation
and patching semantics of Third-Party Resources was supported.

Each Istio configuration object is stored as a Kubernetes Third-Party resource
of kind `istioconfig`. The name of the resource is a combination of Istio kind
and configuration name, and namespaces are shared between Kubernetes and Istio.
Internally, this means Kubernetes assigns a key subset in `etcd` to Istio,
exposes an HTTP endpoint for streaming updates to the store, and provides the
necessary API machinery for creating a cached controller interface.
You should be able to query the key/value store using `kubectl get istioconfigs`.

## 3. Proxy re-configuration

Once a configuration object is persisted in `etcd`, a notification is sent to
the proxy agent controller about the update to the store.  Proxy agent reacts
by creating new proxy configuration localized to the pod in which the agent
runs.  If the configuration is unchanged, the notification is successfully
handled. Otherwise, the agent triggers proxy reconfiguration. One of the future
goals in Istio Pilot, is to provide a feedback loop, to report back to the
store if the proxy fails to configure, or if the proxy fails for some other
reason.



