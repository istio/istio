## Config Model Rule Changes

The following rule resource changes are needed to migrate
from Istio 0.1 (alpha) to Istio 0.2 config format.

### Create Route Rule

0.1.x:
```
istioctl create route-rule -f myrule.yaml
```
0.2.x:
```
kubectl apply -f myrule.yaml
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
  namespace: bar # optional (alternatively could use kubectl -n bar ...)
spec:
  target:
    service: foo
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
      service: foo
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

0.1.x:
```
  prefix: abc
```
0.2.x:
```
  ^abc
```

0.1.x:
```
  regex: abc
```
0.2.x:
```
  /abc/
```

### Example

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
  target:
    service: ratings
  precedence: 2
  match:
    request:
      headers:
        cookie: /^(.*?;)?(user=jason)(;.*)?$/
  route:
  - labels:
      version: v1
  httpFault:
    delay:
      percent: 100
      fixedDelay: 7s
```

### Open Issues

All of the changed names are aligned with the current attibute names
in Mixer, however there are 2 possible changes that need to be decided
and then changed in both places:

1. Should we rename "target" to "destination"
   (e.g. target.labels -> destination.labels)?
2. Need to add "source.service" attribute to Mixer attributes.
   (Alternative is to remove "target.service and use "name" to refer to
    the service on both ends (i.e., "source.name" and "target.name")
