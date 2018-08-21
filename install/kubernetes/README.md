# Install Istio on an existing Kubernetes cluster

Please follow the installation instructions from [istio.io](https://istio.io/docs/setup/kubernetes/quick-start.html).

## Directory structure

If you prefer to install Istio from checking out the [istio/istio](https://github.com/istio/istio) repostiory, you can run `updateVersion.sh` in the parent directory to generate the required installation files.  This directory contains files needed for installing Istio on a Kubernetes cluster:

* istio.yaml - use this generated file for installation without authentication enabled
* istio-auth.yaml - use this generated file for installation with authentication enabled
* templates - directory contains the templates used to generate istio.yaml and istio-auth.yaml
* addons - directory contains optional components (Prometheus, Grafana, Service Graph, Zipkin, Zipkin to Stackdriver)
* helm - directory contains the Istio helm release configuration files.  This directory also requires running `updateVersion.sh` to generate some of the configuration files.
