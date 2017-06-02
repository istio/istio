# Proxy sidecar injection

## Automatic injection

Istio's goal is transparent proxy injection into end-user deployments
with minimal effort from the end-user. Ideally, a kubernetes admission
controller would rewrite specs to include the necessary init and proxy
containers before they are committed, but this currently requires
upstreaming changes to kubernetes which we would like to avoid for
now. Instead, it would be better if a dynamic plug-in mechanism
existed whereby admisson controllers could be maintained
out-of-tree. There is no platform support for this yet, but a proposal
has been created to add such a feature
(see [Proposal: Extensible Admission Control](https://github.com/kubernetes/community/pull/132/)).

Long term istio automatic proxy injection is being tracked
by [Kubernetes Admission Controller for proxy injection](https://github.com/istio/pilot/issues/57).

## Manual injection

A short term workaround for the lack of a proper istio admision
controller is client-side injection. Use `istioctl kube-inject` to add the
necessary configurations to a kubernetes resource files.

    istioctl kube-inject -f deployment.yaml -o deployment-with-istio.yaml

Or update the resource on the fly before applying.

    istioctl kube-inject -f depoyment.yaml | kubectl apply -f -

Or update an existing deployment.

    kubectl get deployment -o yaml | istioctl kube-inject -f - | kubectl apply -f -

`istioctl kube-inject` will update
the [PodTemplateSpec](https://kubernetes.io/docs/api-reference/v1/definitions/#_v1_podtemplatespec) in
kubernetes Job, DaemonSet, ReplicaSet, and Deployment YAML resource
documents. Support for additional pod-based resource types can be
added as necessary.

Unsupported resources are left unmodified so, for example, it is safe
to run `istioctl kube-inject` over a single file that contains multiple
Service, ConfigMap, and Deployment definitions for a complex
application.

The Istio project is continually evolving so the low-level proxy
configuration may change unannounced. When in doubt re-run `istioctl kube-inject`
on your original deployments.

```
$ istioctl kube-inject --help
Inject istio runtime into existing kubernete resources

Usage:
   inject [flags]

Flags:
      --discoveryPort int     Pilot discovery port (default 8080)
  -f, --filename string       Unmodified input kubernetes resource filename
      --initImage string      Istio init image (default "docker.io/istio/init:latest")
      --mixerPort int         Mixer port (default 9091)
  -o, --output string         Modified output kubernetes resource filename
      --runtimeImage string   Istio runtime image (default "docker.io/istio/runtime:latest")
      --sidecarProxyUID int   Sidecar proxy UID (default 1337)
      --verbosity int         Runtime verbosity (default 2)
```
