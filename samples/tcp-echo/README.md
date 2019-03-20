# TCP Echo Service

This sample runs [TCP Echo Server](src/) as an Istio service. TCP Echo Server
allows you to connect to it over TCP and echoes back data sent to it along with
a preconfigured prefix.

## Usage

To run the TCP Echo Service sample:

1. Install Istio by following the [istio install instructions](https://istio.io/docs/setup/kubernetes/quick-start.html).

2. Start the `tcp-echo-server` service inside the Istio service mesh:

   ```console
   $ kubectl apply -f <(istioctl kube-inject -f tcp-echo.yaml)
   service/tcp-echo created
   deployment.extensions/tcp-echo created
   ```

3. Test by running the `nc` command from a `busybox` container from within the cluster.

   ```console
   $ kubectl run -i --rm --restart=Never dummy --image=busybox -- sh -c "echo world | nc tcp-echo 9000"
   hello world
   pod "dummy" deleted
   ```

   As you observe, sending _world_ on a TCP connection to the server results in
   the server prepending _hello_ and echoing back with _hello world_.

4. To clean up, execute the following command:

   ```console
   $ kubectl delete -f tcp-echo.yaml
   service "tcp-echo" deleted
   deployment.extensions "tcp-echo" deleted
   ```
