# Istio Kubernetes Demo with simple echo app

**Prerequisite: existing Kubernetes cluster with Istio installated as per [../../../kubernetes/README.md](../../../kubernetes/README.md).


**Deploy a simple echo app with manually injected proxy**

    kubectl apply <(istioctl kube-inject -f ./demos/apps/echo/echo-app.yaml)
    kubectl apply <(istioctl kube-inject -f ./demos/apps/echo/logic-app.yaml)
    kubectl apply -f ./demos/apps/echo/vanilla-app.yaml

This will deploy two pods, each running a simple echo server and client, and will create two kubernetes services called "echo" and "logic".

**Send some traffic**

Note the pod corresponding to the apps "echo" and "logic":

    kubectl get pods


Send HTTP request from "echo" pod to "logic" service:

    kubectl exec <echo-pod> -c app /bin/client -- -url http://logic/<some-text> -- --count 10

Send HTTP request from "logic" pod to "echo" service:

    kubectl exec -it <logic-pod> -c app /bin/client -- -url http://echo/<some-text> -- --count 10

This will echo the URL and print HTTP headers, including "X-Envoy-Expected-Rq-Timeout-Ms".

**Enable rate limiting in mixer**

    kubectl replace -f ./demos/mixer-config-quota-echo.yaml
