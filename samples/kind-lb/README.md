
# Set up kubernetes cluster with a load balancer

This bash script sets up a k8s cluster using kind and metallb.

The following software are required on your machine to run the script:
  1. kubectl
  2. kind
  3. docker

Usage:

```bash
   ./setupkind.sh --cluster-name cluster1 --k8s-release 1.22.1 --ip-octet 255

Where:
    -n|--cluster-name  - name of the k8s cluster to be created, cluster1 will be used if not given
    -r|--k8s-release   - the release of the k8s to setup, latest available if not given
    -s|--ip-octet      - the 3rd octet for public ip addresses, 255 if not given, valid range: 0-255
    -h|--help          - print the usage of this script
```

The `ip-octet` parameter controls the IP range when metallb gets configured for
allocating public IP address for a load balancer resource. The entire IP segement
is based on the docker network. `KinD` creates a docker network named `kind` when
a k8s cluster is created. This docker network's subnet dictates the public IP
address range for the metallb provided IP addresses. This is not important when
create just one k8s cluster, using default will be sufficient. When creating multiple
k8s clusters on a single machine, it will be important to provide a value which
should be between 1 and 255. For example, to create two clusters, one can invoke
the script two times with the following parameters:

```bash
  ./setupkind.sh --cluster-name cluster1 --ip-octet 255
  ./setupkind.sh --cluster-name cluster2 --ip-octet 245
```

When work with istio external control plane, you can use this script to easily
create multiple k8s clusters with load balancer so that endpoints can be easily
accessible from multiple k8s clusters.
