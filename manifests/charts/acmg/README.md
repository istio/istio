# Istio Acmg Helm Chart

This chart installs the Istio Acmg.

## Installing the Chart

To install the chart with the release name `istio-cni`:

```console
kubectl create namespace istio-system
helm install acmg istio/acmg -n istio-system
```

## Uninstalling the Chart

To uninstall/delete the `acmg` deployment:

```console
helm delete acmg --namespace istio-system
```
