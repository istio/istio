# Install Istio on an existing Kubernetes cluster

Please follow the installation instructions from [istio.io](https://istio.io/docs/setup/kubernetes/quick-start.html).

## Directory structure

If you prefer to install Istio from checking out the [istio/istio](https://github.com/istio/istio) repository, you can run `updateVersion.sh` in the parent directory to generate the required installation files.  This directory contains files needed for installing Istio on a Kubernetes cluster:

* istio-demo.yaml - use this generated Istio demo yaml for installation without authentication enabled
* istio-demo-auth.yaml - use this generated Istio demo yaml for installation with authentication enabled
* helm - directory contains the Istio helm release configuration files. 
* ansible - directory contains the Istio ansible release configuration files.