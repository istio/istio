# TCP Echo Service

This sample runs [TCP Echo Server](src/) as an Istio service. TCP Echo Server
allows you to connect to it over TCP and echoes back data sent to it along with
a preconfigured prefix.

## Usage

To run the TCP Echo Service sample:

1.  Install Istio by following the [Istio install instructions](https://istio.io/docs/setup/kubernetes/quick-start.html).

1.  Start the `tcp-hello-echo` service inside the Istio service mesh.

    If you have [automatic sidecar injection](/docs/setup/kubernetes/additional-setup/sidecar-injection/#automatic-sidecar-injection) enabled, run the following command to deploy the sample app:

    ```console
    $ kubectl apply -f tcp-hello-echo.yaml
    service/tcp-hello-echo created
    deployment.extensions/tcp-hello-echo created
    ```

    Otherwise, manually inject the sidecar before deploying the `tcp-hello-echo` service with the following command:

    ```console
    $ kubectl apply -f <(istioctl kube-inject -f tcp-hello-echo.yaml)
    service/tcp-hello-echo created
    deployment.extensions/tcp-hello-echo created
    ```

1.  Require mutual TLS on connections to `tcp-hello-echo`:

    ```console
    $ cat <<EOF | kubectl apply -f -
    apiVersion: authentication.istio.io/v1alpha1
    kind: Policy
    metadata:
      name: tcp-hello-echo
    spec:
      targets:
      - name: tcp-hello-echo
      peers:
      - mtls: {}
    EOF
    ```

1.  Configure clients to use mutual TLS on connections to `tcp-hello-echo`:

    ```console
    $ kubectl apply -f - <<EOF
    apiVersion: networking.istio.io/v1alpha3
    kind: DestinationRule
    metadata:
      name: tcp-hello-echo
    spec:
      host: tcp-hello-echo
      trafficPolicy:
        tls:
          mode: ISTIO_MUTUAL
    EOF
    ```

1.  Deploy the sleep sample app to use as a test source for sending requests.
    If you have
    [automatic sidecar injection](/docs/setup/kubernetes/additional-setup/sidecar-injection/#automatic-sidecar-injection)
    enabled, run the following command to deploy the sample app:

    ```console
    $ kubectl apply -f ../sleep/sleep.yaml
    ```

    Otherwise, manually inject the sidecar before deploying the `sleep` application with the following command:

    ```console
    $ kubectl apply -f <(istioctl kube-inject -f ../sleep/sleep.yaml)
    ```

1.  Test by running the `nc` command from the `sleep` container from within the cluster.

    ```console
    $ kubectl exec -it $(kubectl get pod -l app=sleep -o jsonpath='{.items..metadata.name}') -c sleep -- sh -c 'echo world | nc tcp-hello-echo 9000'
    hello world
    pod "dummy" deleted
    ```

    As you observe, sending _world_ on a TCP connection to the server results in
    the server prepending _hello_ and echoing back with _hello world_.

1. To clean up, execute the following commands:

    ```console
    $ kubectl delete policy tcp-hello-echo
    $ kubectl delete destinationrule tcp-hello-echo
    $ kubectl delete -f tcp-hello-echo.yaml
    policy.authentication.istio.io "tcp-hello-echo" deleted
    destinationrule.networking.istio.io "tcp-hello-echo" deleted
    service "tcp-hello-echo" deleted
    deployment.extensions "tcp-hello-echo" deleted
    ```

1.  To delete the `sleep` sample, run:

    ```console
    $ kubectl delete -f ../sleep/sleep.yaml
    ```
