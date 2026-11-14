# Upgrade dataset

These files contain rendered CNI manifests to install various CNI versions.
They are `tar`ed to avoid developer confusion and accidental edits.

Revisioned control planes for previous versions are no longer vendored here. `TestRevisionedUpgrade`
installs them from the published Helm charts instead, picking the versions off the `VERSION` file at
test time. See `installRevisionOrFail` in `revisioned_upgrade_test.go`.

## Adding a new CNI version

1. Generate a new CNI Daemonset manifest with the following settings:

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  hub: registry.istio.io/release
  profile: empty
  components:
    cni:
      enabled: true
      namespace: kube-system
```

1. Run `tar cf 1.x.y-cni-install.yaml.tar 1.x.y-cni-install.yaml`
