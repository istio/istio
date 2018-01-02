# Authorization template

Authorization template supports Mixer's core runtime method **Check**.
Istio is adding Authorization support with a list of attributes that identify the caller identity
and how a resource is accessed. As part of that effort, Mixer will add new mesh functions
(defined by Mixer templates) that enables authorization policies.
The new template defines interfaces for Mixer adapters to implement to provide Authorization functionality.

Please see: [Mixer Config](https://istio.io/docs/concepts/policy-and-control/mixer-config.html) for a conceptual overview.

## Template configuration
```protobuf
// A subject contains a list of attributes that identify
// the caller identity.
message Subject {
  // The user name/ID that the subject represents.
  string user = 1;
  // Groups the subject belongs to depending on the authentication mechanism,
  // "groups" are normally populated from JWT claim or client certificate.
  // The operator can define how it is populated when creating an instance of
  // the template.
  string groups = 2;
  // Additional attributes about the subject.
  map<string, istio.mixer.v1.config.descriptor.ValueType> properties = 3;
}

// An action defines "how a resource is accessed".
message Action {
  // Namespace the target action is taking place in.
  string namespace = 1;
  // The Service the action is being taken on.
  string service = 2;
  // What action is being taken.
  string method = 3;
  // HTTP REST path within the service
  string path = 4;
  // Additional data about the action for use in policy.
  map<string, istio.mixer.v1.config.descriptor.ValueType> properties = 5;
}

// The authorization template defines parameters for performing policy
// enforcement within Istio. It is primarily concerned with enabling Mixer
// adapters to make decisions about who is allowed to do what.
// In this template, the "who" is defined in a Subject message. The "what" is
// defined in an Action message. During a Mixer Check call, these values
// will be populated based on configuration from request attributes and
// passed to individual authorization adapters to adjudicate.
message Template {
  // A subject contains a list of attributes that identify
  // the caller identity.
  Subject subject = 1;
  // An action defines "how a resource is accessed".
  Action action = 2;
}
```

## Sample configuration
```yaml
apiVersion: "config.istio.io/v1alpha2"
kind: authorization
metadata:
  name: authinfo
  namespace: istio-system
spec:
  subject:
    user: source.user | request.auth.token[user] | ""
    groups: request.auth.token[groups]
    properties:
      iss: request.auth.token["iss"]
 action:
   namespace: target.namespace | "default"
   service: target.service | ""
   path: request.path | "/"
   method: request.method | "post"
   properties:
     version: destination.labels[version] | ""
```
