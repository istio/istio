# Opa adapter

The OPA authorization [mixer adapter](https://istio.io/docs/concepts/policy-and-control/mixer.html#adapters) is an implementation of [authorization template](https://github.com/istio/istio/tree/master/mixer/template/authorization)
that evaluates the client request using the [Open Policy Agent](http://www.openpolicyagent.org/) engine.

Opa adapter embedded the [Open Policy Agent](http://www.openpolicyagent.org/) as a library inside a Mixer adapter.

![mixer adapter opa](https://github.com/mangchiandjjoe/istio/blob/authorization_opa_adapter_fix/mixer/adapter/opa/mixer_adapter_opa.png?raw=true)

The adapter is responsible for (1) instantiating an OPA instance, (2) passing the parameters to OPA and getting the evaluation results from OPA at runtime

#### Configuration flow (1 and 2 in the above figure):

  1. Service producer sets authorization rules via istioctl. The rules are saved in Istio Configuration server.
  1. The authorization adapter fetches the rules and passes to OPA.

#### Runtime flow (3 and 4 in the above figure):

  1. When a request reaches the authorization handler, the request context is passed to OPA module. The request context can include the following:
     * Client identity (e.g., service account email, user ID, JWT claims).
     * Service name.
     * Request path.
     * Request method.
     * Other request attributes (e.g., query parameters, request headers, pod labels).
  1. OPA evaluates the request context against the rules, and returns the result.

#### Configuration

To activate OPA adapter, operator need to configure the
[authorization template](https://github.com/istio/istio/blob/master/mixer/template/authorization/template.proto) and
[opa adapter](https://github.com/istio/istio/blob/master/mixer/adapter/opa/config/config.proto).

```protobuf
message Params {
 // List of OPA policies
 repeated string policy = 1;

 // Query method to check, data.<package name>.<method name>
 string check_method = 2;

 // Close the client request when adapter has a issue.
 // If failClose is set to true and there is a runtime error,
 // instead of disabling the adapter, close the client request

 bool fail_close = 3;
}
```

#### Example configuration

```yaml
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
 name: authorization
 namespace: istio-config-default
spec:
 selector: "true"
 actions:
 - handler: opaHandler.opa.istio-config-default
   instances:
   - authzInstance.authorization.istio-config-default

---

apiVersion: "config.istio.io/v1alpha2"
kind: authorization
metadata:
 name: authzInstance
 namespace: istio-config-default
spec:
 subject:
   user: source.uid | ""
 action:
   namespace: target.namespace | "default"
   service: target.service | ""
   method: request.method | ""
   path: request.path | ""

---

apiVersion: "config.istio.io/v1alpha2"
kind: opa
metadata:
 name: opaHandler
 namespace: istio-config-default
spec:
 policy:
   - |+
     package mixerauthz
    policy = [
      {
        "rule": {
          "verbs": [
            "storage.buckets.get"
          ],
          "users": [
            "bucket-admins"
          ]
        }
      }
    ]

    default allow = false

    allow = true {
      rule = policy[_].rule
      input.subject.user = rule.users[_]
      input.action.method = rule.verbs[_]
    }
 checkMethod: "data.mixerauthz.allow"
 failClose: true
```
