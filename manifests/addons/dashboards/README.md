# Grafana Dashboards

This folder contains Istio's official Grafana dashboards.
These get publish to [Grafana](https://grafana.com/orgs/istio/dashboards) during release, and are bundled into our
[Grafana sample](../../../samples/addons/grafana.yaml).

## Jsonnet

Newer dashboards are generated with [Jsonnet](https://jsonnet.org/) with the [Grafonnet](https://grafana.github.io/grafonnet/index.html).
More info on development workflow of these dashboards can be found [here](https://blog.howardjohn.info/posts/grafana-dashboard-dev/).
This is the preferred method for any new dashboards.

## Dashboard Linking

All dashboards must use UID-based linking rather than path-based linking. This is because:
1. Path-based linking is deprecated in newer Grafana versions
2. UID-based linking is more stable across Grafana upgrades
3. UID-based linking works with both self-hosted and Grafana Cloud instances

We use the MD5 hash of the dashboard filename as the UID, for example:
```jsonnet
g.dashboard.withUid(std.md5('istio-mesh.json'))
```

For linking between dashboards, use the helper libraries:
- `istio-service.libsonnet` - For linking to the Service dashboard
- `istio-workload.libsonnet` - For linking to the Workload dashboard

Example of using dashboard links:
```jsonnet
local serviceDashboard = import 'istio-service.libsonnet';

dashboard.new('My Dashboard')
+ g.dashboard.withLinks([
  serviceDashboard.dashboardLink
])
```

## Legacy Dashboards

Many of our older dashboards are manually created in the UI and exported as JSON and checked in.

## Generation

For both dashboard type, run `./manifests/addons/gen.sh` to generate all required output.

After generation, you can verify that all dashboards are using UID-based linking by running:
```bash
./manifests/addons/dashboards/test_dashboard_links.sh
```
