# Installing Istio on OpenShift using Helm

> Note: Be aware of the [platform setup required for OpenShift](https://istio.io/latest/docs/setup/platform-setup/openshift/) when installing Istio.

To install with Helm, you must first create the namespace that you wish to install in if the namespace does not exist already. The default namespace used is `istio-system` and can be created as follows:

```console
kubectl create namespace istio-system
```

The installation process using the Helm charts is as follows:

1) `base` chart creates cluster-wide CRDs, cluster bindings and cluster resources. It is possible to change the namespace from `istio-system` but it is not recommended.

```console
helm install istio-base -n istio-system manifests/charts/base
```

2) `istio-cni` chart installs the CNI plugin. This should be installed after the `base` chart and prior to `istiod` chart. Need to add `--set istio_cni.enabled=true` to the `istiod` install to enable its usage.

```console
helm install istio-cni -n kube-system manifests/charts/istio-cni -f manifests/charts/global.yaml --set cni.cniBinDir="/var/lib/cni/bin" --set cni.cniConfDir="/etc/cni/multus/net.d" --set cni.chained=false --set cni.cniConfFileName="istio-cni.conf" --set cni.excludeNamespaces[0]="istio-system" --set cni.excludeNamespaces[1]="kube-system" --set cni.repair.enabled=false --set cni.logLevel=info
```

3) `istio-control/istio-discovery` chart installs a revision of istiod.

```console
 helm install -n istio-system istio-17 manifests/charts/istio-control/istio-discovery --set istio_cni.enabled=true --set global.jwtPolicy=first-party-jwt --set sidecarInjectorWebhook.injectedAnnotations."k8s\.v1\.cni\.cncf\.io/networks"="istio-cni"
```

4) `gateways` charts install a load balancer with `ingress` and `egress`.

Ingress secrets and access should be separated from the control plane.

```console
helm install -n istio-system istio-ingress manifests/charts/gateways/istio-ingress -f manifests/charts/global.yaml --set global.jwtPolicy=first-party-jwt
```

Egress secrets and access should be separated from the control plane.

```console
helm install -n istio-system istio-egress manifests/charts/gateways/istio-egress -f manifests/charts/global.yaml --set global.jwtPolicy=first-party-jwt
```

