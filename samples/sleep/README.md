# Simple sleep service

This sample consists of a simple service that does nothing but sleep.
It's a ubuntu container with curl installed that can be used as a request source for invoking other services
to experiment with Istio networking.

To use it:

1. Install Istio by following the [istio install instructions](https://istio.io/docs/setup/).

1. Start the sleep service:

    If you have [automatic sidecar injection](https://istio.io/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection) enabled:

    ```bash
    kubectl apply -f sleep.yaml
    ```

    Otherwise manually inject the sidecars before applying:

    ```bash
    kubectl apply -f <(istioctl kube-inject -f sleep.yaml)
    ```

1. Start some other services, for example, the [Bookinfo sample](https://istio.io/docs/examples/bookinfo/).

    Now you can `kubectl exec` into the sleep service to experiment with Istio networking.
    For example, the following commands can be used to call the Bookinfo `ratings` service:

    ```bash
    export SLEEP_POD=$(kubectl get pod -l app=sleep -o jsonpath={.items..metadata.name})
    kubectl exec -it $SLEEP_POD -c sleep -- curl http://ratings.default.svc.cluster.local:9080/ratings/1
    {"id":1,"ratings":{"Reviewer1":5,"Reviewer2":4}}
    ```

You can also use the sleep service to test accessing services outside of the mesh.
See [configuring egress](https://istio.io/docs/tasks/traffic-management/egress/) for details.
