# Simple curl service

This sample is a a request source for invoking other services, to experiment with Istio networking.
It consists of a pod that does nothing but sleep. You can get a shell on the pod (an Alpine container) and use `curl`.

To use it:

1. Install Istio by following the [installation instructions](https://istio.io/docs/setup/).

1. Start the curl pod:

    ```bash
    kubectl apply -f curl.yaml
    ```

1. Start some other services, for example, the [Bookinfo sample](https://istio.io/docs/examples/bookinfo/).

    Now you can `kubectl exec` into the curl service to experiment with Istio networking.
    For example, the following commands can be used to call the Bookinfo `ratings` service:

    ```bash
    export CURL_POD=$(kubectl get pod -l app=curl -o jsonpath={.items..metadata.name})
    kubectl exec -it $CURL_POD -c curl -- curl http://ratings.default.svc.cluster.local:9080/ratings/1
    {"id":1,"ratings":{"Reviewer1":5,"Reviewer2":4}}
    ```

You can also use the curl service to test accessing services outside of the mesh.
See [configuring egress](https://istio.io/docs/tasks/traffic-management/egress/) for details.
