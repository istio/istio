# Istio [Grafana](https://grafana.com/) image

This image is Grafana configured to work with Istio, and a few
pre-loaded dashboards:

### Istio Dashboard
This is the primary dashboard with an overview of the whole mesh.

### Mixer Dashboard
This dashboard focuses on
[Mixer](https://istio.io/docs/concepts/policy-and-control/mixer.html).

### Pilot Dashboard
This dashboard focuses on
[Pilot](https://istio.io/docs/concepts/traffic-management/pilot.html).

## Implementation details

[`start.sh`](start.sh) is the entrypoint, and it runs
[`import_dashboard.sh`](import_dashboard.sh) in the background then
starts Grafana. `import_dashboard.sh` POSTs the included dashboards
and Prometheus data-source config to Grafana once it starts up.

A few env vars are set in the [Dockerfile](Dockerfile) to configure
Grafana, and more environment-specific ones are set in the [install
config](/install/kubernetes/templates/addons/grafana.yaml.tmpl).
