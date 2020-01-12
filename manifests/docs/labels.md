= Upgrade and deployment labels

The most frequent problem in Istio with 'upgrade in place' is the label missmatch for deployments.

This happens when the upgrade Deployment.template.metadata.labels object on the upgrade doesn't matches
the previous version.

Example error:

```text
for: "test/demo": Deployment.apps "egressgateway" is invalid: spec.selector: Invalid value: v1.LabelSelector{MatchLabels:map[string]string{"app":"istio-egressgateway", "istio":"egressgateway"}, MatchExpressions:[]v1.LabelSelectorRequirement(nil)}: field is immutable
```

For Istio 1.0, the label style was:

```yaml
...
  labels:
      app: pilot
      istio: pilot
```

'matchLabels' is only used in prometheus, with 'app:prometheus'.

In 1.1, we added 3 more labels:

```yaml
  template:
    metadata:
      labels:
        app: pilot
        istio: pilot

        chart: pilot
        heritage: Tiller
        release: istio
```

For 1.2, we want to stop adding 'chart'/'heritage'/release, to reduce the dependency on Helm/Tiller and avoid
similar problems in the future.

We also want to allow in-place update of istio-system, for demo or users who need this (as a backup
plan).

As such, the deployments have special code to generate the Tiller-related labels - but only if the install
is done in istio-system, in 'legacy' mode.
