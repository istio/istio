# Install Istio on an existing Kubernetes cluster

Please follow the installation instructions from [istio.io](https://istio.io/docs/setup/kubernetes/quick-start.html).

## Directory structure
This directory contains files needed for installing Istio on a Kubernetes cluster.

* istio.yaml - use this file for installation without authentication enabled
* istio-auth.yaml - use this file for installation with authentication enabled
* istio-initializer.yaml - use this file for installation of istio initializer for transparent injection.
* istio-one-namespace.yaml - use this file for installation without authentication enabled and CA. Istio control plane and applications will be in one single namespace, mainly used for testing.
* istio-one-namespace-auth.yaml - use this file for installation without authentication enabled and CA. Istio control plane and applications will be in one single namespace, mainly used for testing.
* templates - directory contains the templates used to generate istio.yaml and istio-auth.yaml
* addons - directory contains optional components (Prometheus, Grafana, Service Graph, Zipkin, Zipkin to Stackdriver)
* updateVersion.sh in the parent directory can be run to regenerate installation files
