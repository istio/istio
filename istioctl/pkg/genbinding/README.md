# Multi mesh support

This command line utility generates the resource required for using Istio
across multiple meshes.

## Usage

```
istioctl experimental gen-binding <service:port> --cluster <ip:port> [--cluster <ip:port>]*  [--labels key1=value1,key2=value2]  [--use-egress] [--egressgateway <ip:port>]
```

## Examples:

Accessing the service _reviews_ on a remote mesh by specifying the address of the ingress gateway on the remote cluster:

```
istioctl experimental gen-binding reviews:9080 --cluster 1.2.3.4:15443
```

Accessing a service on multiple remote clusters:

```
istioctl experimental gen-binding reviews:9080 --cluster 1.2.3.4 --cluster 6.7.8.9
```

Specifying one or more labels for the service entry (used for subsets):

```
istioctl experimental gen-binding reviews:9080 --cluster 1.2.3.4:15443 --labels version=v1

istioctl experimental gen-binding ratings:8080 --cluster 1.2.3.4 --labels version=v1,arch=i586
```

Using an egress gateway on local cluster for accessing the remote cluster:

```
istioctl experimental gen-binding reviews:9080 --cluster 1.2.3.4 --use-egress

istioctl experimental gen-binding reviews:9080 --cluster 1.2.3.4 --egressgateway 6.7.8.9:15443

istioctl experimental gen-binding reviews:9080 --cluster 1.2.3.4 --egressgateway 6.7.8.9

istioctl experimental gen-binding ratings:8080 --cluster 1.2.3.4 --labels version=v1,arch=i586 --use-egress
```

