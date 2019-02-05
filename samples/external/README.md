# External Services

Istio-enabled services are unable to access services and URLs outside of the cluster. Pods use <i>iptables</i> to transparently redirect all outbound traffic to the sidecar proxy, which only handles intra-cluster destinations.

See [the Egress Task](https://istio.io/docs/tasks/traffic-management/egress/) for
information on Configuring Istio to contact external services.

This directory contains samples showing how to enable pods to contact a few well
known services.

If Istio is not configured to allow pods to contact external services the pods will
see errors such as 404s, HTTPS connection problems, and TCP connection problems.  If
ServiceEntries are misconfigured pods may see problems with server names.

## Try it out

After an operator runs `kubectl create -f aptget.yaml` pods will be able to
succeed with `apt-get update` and `apt-get install`.

After an operator runs `kubectl create -f github.yaml` pods will be able to
succeed with `git clone`.

Running `kubectl create -f pypi.yaml` allows pods to update Python libraries using `pip`.

It is not a best practice to enable pods to update libraries dynamically.
We are providing these samples
because they have proven to be helpful with interactive troubleshooting.  Security minded clusters should only allow traffic to service dependencies such as cloud
services.
