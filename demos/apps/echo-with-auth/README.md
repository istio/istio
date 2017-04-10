# Istio Kubernetes Demo with simple echo app

**Install Istio on a Kubernetes cluster**

1. Install Istio Manager
    ```bash
    kubectl apply -f istio-manager.yaml
    ```

    Notice in the YAML file, the manager has been configured to use mutual TLS connections between Istio proxies
    ```yaml
    data:
      mesh: |-
        authPolicy: 1                        # Enables Istio auth
        mixerAddress: istio-mixer:9091
        discoveryAddress: istio-manager:8080
    ```
1. Install Istio Mixer
    ```bash
    kubectl apply -f istio-mixer.yaml
    ```
1. Install Istio CA
    ```bash
    kubectl apply -f istio-ca.yaml
    ```

**Deploy a simple echo app with manually injected proxy**
```bash
kubectl apply -f <(istioctl kube-inject -f echo-app.yaml --tag 2017-04-08-23.03.08)
kubectl apply -f <(istioctl kube-inject -f logic-app.yaml --tag 2017-04-08-23.03.08)
```

This will deploy two pods, each running a simple echo server and client, and
will create two kubernetes services called "echo" and "logic".

**Send some traffic**

Note the pod corresponding to the apps "echo" and "logic":
```bash
kubectl get pods
```

Wait until all pods are in the `running` state.

Send HTTP request from "echo" pod to "logic" service:
```bash
kubectl exec $(kubectl get po -l app=echo -o jsonpath='{.items[0].metadata.name}') -c app /bin/client \
    -- -url http://logic/sample-text -- --count 10
```

Send HTTP request from "logic" pod to "echo" service:
```bash
kubectl exec $(kubectl get po -l app=logic -o jsonpath='{.items[0].metadata.name}') -c app /bin/client \
    -- -url http://echo/sample-text -- --count 10
```
