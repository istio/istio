# Install Istio on an existing Kubernetes cluster

Please follow the installation instructions from [istio.io](https://istio.io/docs/tasks/installing-istio.html).

## Directory structure
This directory contains files needed for installing Istio on a Kubernetes cluster.

* istio.yaml - use this file for installation without authentication enabled
* istio-auth.yaml - use this file for installation with authentication enabled
* istio-cluster-wide.yaml - use this file for installation cluster-wide with authentication enabled
* templates - directory contains the templates used to generate istio.yaml and istio-auth.yaml
* addons - directory contains optional components (Prometheus, Grafana, Service Graph, Zipkin, Zipkin to Stackdriver)
* updateVersion.sh in the parent directory can be run to regenerate installation files
