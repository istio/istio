# proxyless gRPC examples

## Using the templates

Each of the `yaml` files in this directory can be added to the `istio-sidecar-injector` ConfigMap.
They add custom inject templates that will bootstrap a container to allow gPRC's XDS itegration to
communicate with `istiod`.

1. Choose a template:

```bash
TEMPLATE=<grpc-simple|grpc-agent>
```

2. Install the template:

```bash
# get the orignal template; remove resource version to make it easy to restore the orignal
kubectl -n istio-system get cm istio-sidecar-injector -oyaml | grep -v resourceVersion > original-injector-cm.yaml
# patch the ConfigMap with the new template
sed "/templates:/ r samples/proxyless-grpc/$TEMPLATE.yaml" original-injector-cm.yaml > custom-injector-cm.yaml
# apply it to the cluster
kubectl apply -f custom-injector-cm.yaml
```

2. Use the template in a deployment:

Create/modify any Deployment/Pod so that it has the `inject.istio.io` annotation with the relevant template name.

### grpc-simple

Uses an init container to create the bare minimum bootstrap required for gRPC to get XDS from `istiod`.
Communication to the control-plane and the data-plane will be insecure.

### grpc-agent

A stripped down version of the usual `sidecar` injection template. A sidecar running`istio-agent` will only be used
for the following:

* Generating gRPC bootstrap with the complete set of metadata `istiod` needs
* Managing certs for control-plane and data-plane communication
* Acting as an XDS proxy to `istiod`

This template does *not*:
* Run `istio-iptables` init container
* Run Envoy via the agent.
