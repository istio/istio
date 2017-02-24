# Proxy sidecar injection

## Manual injection

Add the following annotation to the deployment's template metadata.

```
annotations:
  pod.beta.kubernetes.io/init-containers: >
    [{
      "name": "init-proxy",
      "image": "docker.io/istio/init:latest",
      "securityContext": { "capabilities" : { "add" : ["NET_ADMIN"] } }
    }]
```
    
Add the following container to the deployment's template spec. The UID '1337' should match ISTIO_PROXY_UID as specified by [proxy-redirection-configuration.md](proxy-redirection-configuration.md )

```
- name: proxy
  image: docker.io/istio/runtime:latest
  securityContext:
    runAsUser: 1337
  args:
    - proxy
    - -s
    - manager:8080
```  

A sample deployment with manually injected init and proxy containers might look like this.

```
apiVersion: v1
kind: Service
metadata:
  name: example
  labels:
    app: example
spec:
  type: NodePort
  ports:
  - port: 80
    targetPort: 8080
    name: http
  selector:
    app: example
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: example
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: example
      annotations:
        pod.beta.kubernetes.io/init-containers: >
          [{
            "name": "proxy-init",
            "image": "docker.io/istio/init:latest",
            "securityContext": { "capabilities" : { "add" : ["NET_ADMIN"] } }
          }]
    spec:
      containers:
      - name: app
        image: docker.io/istio/app:latest
        args:
          - --port
          - "8080"
        ports:
        - containerPort: 8080
      - name: proxy
        image: docker.io/istio/runtime:latest
        securityContext:
          runAsUser: 1337
        args:
          - proxy
          - -s
          - manager:8080
---
```

## Automatic injection

Istio's goal is transparent proxy injection into end-user deployments with minimal effort from the end-user. Ideally, a kubernetes admission controller would rewrite specs to include the necessary init and proxy containers before they are committed, but his currently requires upstreaming changes to kubernetes which we would like to avoid for now. Instead, it would be better if a dynamic plug-in mechanism existed whereby admisson controllers could be maintained out-of-tree. There is no platform support for this yet, but a proposal has been created to add such a feature (see [Proposal: Extensible Admission Control](https://github.com/kubernetes/community/pull/132/)). 

Long term istio automatic proxy injection is being tracked by [Kubernetes Admission Controller for proxy injection](https://github.com/istio/manager/issues/57).


