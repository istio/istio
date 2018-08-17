## Config Model Rule Changes

The following rule resource changes are needed to migrate
from Istio 0.1 (alpha) to Istio 0.2 config format.

Note that all of the 0.2 Pilot config property names are now aligned with the
[attibute vocabulary](https://istio.io/docs/reference/config/mixer/attribute-vocabulary.html)
used for Mixer config.

### Create Route Rule

0.1.x:
```
istioctl create route-rule -f myrule.yaml
```
0.2.x:
```
istioctl create -f myrule.yaml

    or (for Kubernetes users):

kubectl create -f myrule.yaml
```

### Route Rule YAML

0.1.x:
```
```
0.2.x:
```
apiVersion: config.istio.io/v1alpha2
```

0.1.x:
```
type: route-rule
```
0.2.x:
```
kind: RouteRule
```

0.1.x:
```
name: myRule
```
0.2.x:
```
metadata:
  name: myRule
```

0.1.x:
```
spec:
  destination: foo.bar.svc.cluster.local
```
0.2.x:
```
metadata:
  namespace: bar # optional (alternatively could use istioctl -n bar ...)
spec:
  destination:
    name: foo
    namespace: bar # optional
```

0.1.x:
```
spec:
  match:
    httpHeaders:
```
0.2.x:
```
spec:
  match:
    request:
      headers:
```

0.1.x:
```
spec:
  match:
    source: foo.bar.svc.cluster.local
```
0.2.x:
```
spec:
  match:
    source:
      name: foo
      namespace: bar (optional - default is rule namespace)
```

0.1.x:
```
spec:
  match:
    sourceTags:
```
0.2.x:
```
spec:
  match:
    source:
      labels:
```

0.1.x:
```
spec:
  route:
  - tags:
```
0.2.x:
```
spec:
  route:
  - labels:
```

0.1.x:
```
  exact: abc
```
0.2.x:
```
  abc
```

### Create Destination Policy

0.1.x:
```
istioctl create destination-policy -f mypolicy.yaml
```
0.2.x:
```
istioctl create -f mypolicy.yaml

    or (for Kubernetes users):

kubectl create -f mypolicy.yaml
```

### Destination Policy YAML

0.1.x:
```
```
0.2.x:
```
apiVersion: config.istio.io/v1alpha2
```

0.1.x:
```
spec:
  destination: foo.bar.svc.cluster.local
```
0.2.x:
```
metadata:
  namespace: bar # optional (alternatively could use istioctl -n bar ...)
spec:
  destination:
    name: foo
```

0.1.x:
```
spec:
  policy:
  - tags:
```
0.2.x:
```
spec:
  destination:
    labels:
```

### Examples

0.1.x
```
type: route-rule
name: ratings-test-delay
spec:
  destination: ratings.default.svc.cluster.local
  precedence: 2
  match:
    httpHeaders:
      cookie:
        regex: "^(.*?;)?(user=jason)(;.*)?$"
  route:
  - tags:
      version: v1
  httpFault:
    delay:
      percent: 100
      fixedDelay: 7s
```

0.2.x:
```
apiVersion: config.istio.io/v1alpha2
kind: RouteRule
metadata:
  name: ratings-test-delay
spec:
  destination:
    name: ratings
  precedence: 2
  match:
    request:
      headers:
        cookie:
          regex: ^(.*?;)?(user=jason)(;.*)?$
  route:
  - labels:
      version: v1
  httpFault:
    delay:
      percent: 100
      fixedDelay: 7s
```

0.1.x:
```
type: destination-policy
name: reviews-cb
spec:
  destination: reviews.default.svc.cluster.local
  policy:
  - tags:
      version: v1
    circuitBreaker:
      simpleCb:
        maxConnections: 100
```
0.2.x:
```
apiVersion: config.istio.io/v1alpha2
kind: DestinationPolicy
metadata:
  name: reviews-cb
spec:
  destination:
    name: reviews
    labels:
      version: v1
  circuitBreaker:
    simpleCb:
      maxConnections: 100
```
