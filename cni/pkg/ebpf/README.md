# Ambient ebpf redirection

## Build dependencies

clang,llvm and libbpf-dev

## Build Steps

Run `go generate` under ebpf/server to generate bpf go skeletons

## Mandatory configuration for integrating with calico

Calico CNI enables RPF by default(with iptables), thus may DROP some tproxyed packets.

Here is the rpf related change in calico:

[calico/issues/5643](https://github.com/projectcalico/calico/issues/5643)

[calico/pull/5742](https://github.com/projectcalico/calico/pull/5742)

Steps to allow ztunnel "spoofing" the source ip:
1. For calico side, refer to [felix/configuration](https://docs.tigera.io/calico/3.25/reference/felix/configuration)
   1. For deployed by operator,  `kubectl patch felixconfigurations default --type='json' -p='[{"op": "add", "path": "/spec/workloadSourceSpoofing", "value": "Any"}]'`
   2. For deployed by manifest, add env `FELIX_WORKLOADSOURCESPOOFING` with value `Any` in `spec.template.spec.containers.env` for daemonset `calico-node`. (This will allow PODs with specified annotation to skip the rpf check. )
2. For istio side, add  `cni.projectcalico.org/allowedSourcePrefixes: '["<POD_CIDR>"]'` in `spec.template.metadata.annotation` for daemonset `ztunnel`. (This will skip the specified CIDR rpf check, the `<POD_CIDR>` could get by `kubectl get cm -n kube-system kubeadm-config -o jsonpath='{.data.ClusterConfiguration}' | grep podSubnet`).

   *For instance:* `kubectl -n istio-system patch ds ztunnel --type='json'  -p='[{"op": "add", "path": "/spec/template/metadata/annotations/cni.projectcalico.org~1allowedSourcePrefixes", "value": "[\"10.244.0.0/16\"]"}]'`

*Debug Tips*:

* You can confirm if the above configuration is taking effect by checking if there is any related ***ACCEPT*** rule using the command `iptables -t raw -vL cali-rpf-skip`.

* Moreover, may confirm that `/proc/sys/net/ipv4/conf/all/rp_filter` and `/proc/sys/net/ipv4/conf/<intf>/rp_filter` are all disabled(set to 0).
