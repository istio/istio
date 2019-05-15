# External Services

By default Istio-enabled services are unable to access services and URLs outside of the cluster. Pods use <i>iptables</i> to transparently redirect all outbound traffic to the sidecar proxy, which only handles intra-cluster destinations.

See [the Egress Task](https://istio.io/docs/tasks/traffic-management/egress/) for
information on configuring Istio to contact external services.

This directory contains samples showing how to enable pods to contact a few well
known services.

If Istio is not configured to allow pods to contact external services, the pods will
see errors such as 404s, HTTPS connection problems, and TCP connection problems.  If
ServiceEntries are misconfigured pods may see problems with server names.

## Try it out

After an operator runs `kubectl create -f aptget.yaml` pods will be able to
succeed with `apt-get update` and `apt-get install`.

After an operator runs `kubectl create -f github.yaml` pods will be able to
succeed with `git clone https://github.com/fortio/fortio.git`.

Running `kubectl create -f pypi.yaml` allows pods to update Python libraries using `pip`.

It is not a best practice to enable pods to update libraries dynamically.
We are providing these samples
because they have proven to be helpful with interactive troubleshooting.  Security minded clusters should only allow traffic to service dependencies such as cloud
services.

### Enable communication by default

Note that [this note](https://istio.io/docs/tasks/traffic-management/egress/#install-istio-with-access-to-all-external-services-by-default) shows how to configure Istio to contact services by default.  The technique
discussed there does not allow HTTP on port 80 or SSH on port 22.  These examples will
allow external communication for ports 80 and 22.
