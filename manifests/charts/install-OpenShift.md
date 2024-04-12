# Installing Istio on OpenShift using Helm

> Note: Be aware of the [platform setup required for OpenShift](https://istio.io/latest/docs/setup/platform-setup/openshift/) when installing Istio.

To install with Helm, you must first create the namespace that you wish to install in if the namespace does not exist already. The default namespace used is `istio-system` and can be created as follows:

```console
kubectl create namespace istio-system
```

Istio's helm charts come with a common `openshift` profile that can be used during installation.

The installation process using the Helm charts is as follows:

1) `base` chart creates cluster-wide CRDs, cluster bindings and cluster resources. It is possible to change the namespace from `istio-system` but it is not recommended.

```console
helm install istio-base -n istio-system manifests/charts/base --set profile=openshift
```

2) `istio-cni` chart installs the CNI plugin. This should be installed after the `base` chart and prior to `istiod` chart. Need to add `--set pilot.cni.enabled=true` to the `istiod` install to enable its usage.

```console
helm install istio-cni -n kube-system manifests/charts/istio-cni --set profile=openshift
```

3) `istio-control/istio-discovery` chart installs a revision of istiod.

```console
 helm install -n istio-system istiod manifests/charts/istio-control/istio-discovery --set profile=openshift
```

4) `gateways` charts install a load balancer with `ingress` and `egress`.

Ingress secrets and access should be separated from the control plane.

```console
helm install -n istio-system istio-ingress manifests/charts/gateways/istio-ingress --set profile=openshift
```

Egress secrets and access should be separated from the control plane.

```console
helm install -n istio-system istio-egress manifests/charts/gateways/istio-egress --set profile=openshift
```
