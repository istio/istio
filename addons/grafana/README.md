# Istio [Grafana](https://grafana.com/) image

This image is Grafana configured to work with Istio, and a few
pre-loaded dashboards:

### Istio Mesh Dashboard
This is the primary dashboard with an overview of the whole mesh. Workload names
in the workload tables are hyperlinked to individual workload dashboards.

### Istio HTTP/GRPC Workload Dashboard
Provides a dashboard for an individual HTTP or GRPC workload in the mesh.

### Istio TCP Workload Dashboard
Provides a dashboard for an individual TCP workload in the mesh.

### Mixer Dashboard
This dashboard focuses on
[Mixer](https://istio.io/docs/concepts/policy-and-control/mixer.html).

### Pilot Dashboard
This dashboard focuses on
[Pilot](https://istio.io/docs/concepts/traffic-management/pilot.html).

## Implementation details

Grafana config is stored in
[`grafana.ini`](grafana.ini). [`dashboards.yaml`](dashboards.yaml) and
[`datasources.yaml`](datasources.yaml) configure those resources
respectively using the
[provisioning](http://docs.grafana.org/administration/provisioning/)
config.

Provisioning is currently a new feature, so dashboards require a small
modification. Normally when exporting a dashboard from Grafana, the
datasources are templated, and then Grafana un-templates on import by
matching to an existing datasource. When using Provisioning this
doesn't happen, so this needs to be done manually.

The name of the configured datasource in `datasources.yaml` is
`Prometheus`, so all instances of `${DS_PROMETHEUS}` in exported
dashboards need to be replaced with
`Prometheus`. [`fix_datasources.sh`](fix_datasources.sh) accomplishes
this. Grafana is expected to fix this issue in the near future.

Another modification to dashboards is the `"uid"` field. This is
hard-coded instead of using the randomly generated one. This is to
allow easy direct hyperlinks to dashboards.

Environment-specific config is set in the [install
config](/install/kubernetes/helm/istio/values.yaml).
