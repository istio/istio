# Grafana Dashboards

This folder contains Istio's official Grafana dashboards.
These get publish to [Grafana](https://grafana.com/orgs/istio/dashboards) during release, and are bundled into our
[Grafana sample](../../../samples/addons/grafana.yaml).

## Jsonnet

Newer dashboards are generated with [Jsonnet](https://jsonnet.org/) with the [Grafonnet](https://grafana.github.io/grafonnet/index.html).
More info on development workflow of these dashboards can be found [here](https://blog.howardjohn.info/posts/grafana-dashboard-dev/).
This is the preferred method for any new dashboards.

## Legacy Dashboards

Many of our older dashboards are manually created in the UI and exported as JSON and checked in.

## Generation

For both dashboard type, run `./manifests/addons/gen.sh` to generate all required output.
