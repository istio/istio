# Upgrade dataset

These files contain fully rendered manifests to install various Istio versions,
and rendered CNI manifests to install various CNI versions.
They are `tar`ed to avoid developer confusion and accidental edits.

## Adding a new version

1. Generate a revisioned IstioOperator for the version with equivalent settings:

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  hub: gcr.io/istio-release
  revision: 1-x-y
  components:
    base:
      enabled: false
    pilot:
      enabled: true
    ingressGateways:
      - name: istio-ingressgateway
        enabled: false

  values:
    global:
      proxy:
        resources:
          requests:
            cpu: 10m
            memory: 40m
```

1. Run `tar cf 1.x.y-install.yaml.tar 1.x.y-install.yaml`

1. Generate a new CNI Daemonset manifest with the following settings:

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  hub: gcr.io/istio-release
  profile: empty
  components:
    cni:
      enabled: true
      namespace: kube-system
```

1. Run `tar cf 1.x.y-cni-install.yaml.tar 1.x.y-cni-install.yaml`
