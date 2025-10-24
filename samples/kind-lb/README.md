
# Create a KinD cluster with an external load balancer

This bash script sets up a k8s cluster using kind and metallb on Linux.

WARNING: when it runs in an environment other than Linux, it will only
produce an error message.

The following dependencies must be installed before running the script:
  1. kubectl
  2. kind
  3. docker

Usage:

```bash
   ./setupkind.sh --cluster-name cluster1 --k8s-release 1.22.1 --ip-space 255 -i dual

Where:
  -n|--cluster-name  - name of the k8s cluster to be created
  -r|--k8s-release   - the release of the k8s to setup, latest available if not given
  -s|--ip-space      - the 2nd to the last part for public ip addresses, 255 if not given, valid range: 0-255.
  -m|--mode          - setup the required number of nodes per deployment model. Values are sidecar (1 node) or ambient (minimum of 2)
  -w|--worker-nodes  - the number of worker nodes to create. Default is 1
  --pod-subnet       - the pod subnet to specify. Default is 10.244.0.0/16 for IPv4 and fd00:10:244::/56 for IPv6
  --service-subnet   - the service subnet to specify. Default is 10.96.0.0/16 for IPv4 and fd00:10:96::/112 for IPv6
  -i|--ip-family     - ip family to be supported, default is ipv4 only. Value should be ipv4, ipv6, or dual
  --ipv6gw          - set ipv6 as the gateway, necessary for dual-stack IPv6-preferred clusters
  -h|--help          - print the usage of this script"
```

The `ip-space` parameter controls the IP range that metallb will use for allocating
the public IP addresses for load balancer resources. It will be used for both IPv4
and IPv6 public IP address allocation.

The public IPs are dictated by the docker network's subnet which `KinD` creates
when it creates a k8s cluster. This parameter is used as the 3rd octet for the
public IP v4 addresses when a load balancer is created. The default value is 255.
The first two octets are determined by the docker network created by `KinD`, the 4th octet
is hard coded as 200-240. As you might have guessed, for each k8s cluster one can
create at most 40 public IP v4 addresses.

The `ip-space` parameter is not required when you create just one cluster, however, when
running multiple k8s clusters it is important to provide different values for each cluster
to avoid overlapping addresses.

For example, to create two clusters, run the script two times with the following
parameters:

```bash
  ./setupkind.sh --cluster-name cluster1 --ip-space 255 -i dual
  ./setupkind.sh --cluster-name cluster2 --ip-space 245 -i dual
```

To setup a dual-stack cluster that prefers IPv6 instead of IPv4 (the default)

```bash
  ./setupkind.sh --cluster-name cluster2 --ip-space 245 -i dual --pod-subnet "fd00:100:96::/48,100.96.0.0/11" --service-subnet "fd00:100:64::/108,100.64.0.0/13" --ipv6gw
```
