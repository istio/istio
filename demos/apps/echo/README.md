# Istio Kubernetes Demo with simple echo app

**Prerequisite: Install Istio**

Follow the instructions listed in
[../../../kubernetes/README.md](../../../kubernetes/README.md) to install Isito
on an existing Kubernetes cluster.

***Optional: enable mutual TLS between proxies***

Before you install Istio, you can uncomment the following line in
`istio-manager.yaml` file to enable mutual TLS connections among proxies.
```yaml
data:
  mesh: |-
    # Uncomment the following line to enable mutual TLS between proxies
    authPolicy: MUTUAL_TLS
    mixerAddress: istio-mixer:9091
    discoveryAddress: istio-manager:8080
```

Deploy Istio CA for the namespace:
```bash
kubectl apply -f ./kubernetes/istio-auth/istio-namespace-ca.yaml
```

**Deploy a simple echo app with manually injected proxy**

    kubectl apply -f <(istioctl kube-inject -f echo-app.yaml)
    kubectl apply -f <(istioctl kube-inject -f logic-app.yaml)

This will deploy two pods, each running a simple echo server and client, and will create two kubernetes services called "echo" and "logic".

**Send some traffic**

Note the pod corresponding to the apps "echo" and "logic":

    kubectl get pods


Send HTTP request from "echo" pod to "logic" service:

    kubectl exec <echo-pod> -c app /bin/client -- -url http://logic/<some-text> -- --count 10

Send HTTP request from "logic" pod to "echo" service:

    kubectl exec <logic-pod> -c app /bin/client -- -url http://echo/<some-text> -- --count 10

This will echo the URL and print HTTP headers, including "X-Envoy-Expected-Rq-Timeout-Ms".

**Enable rate limiting in mixer**

    kubectl replace -f ./demos/mixer-config-quota-echo.yaml

**Add a third app without istio proxy**

    kubectl apply -f vanilla-app.yaml

This demonstrates apps without proxy can live with the ones with proxy.
