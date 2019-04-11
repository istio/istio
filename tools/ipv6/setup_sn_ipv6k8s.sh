#!/bin/bash -e

#
# Load common  waiting for control plane pods function
#
. ./tools/ipv6/wait_for_control_plane.sh

sudo curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo apt-key add -
sudo bash -c 'echo "deb http://apt.kubernetes.io/ kubernetes-xenial main" > /etc/apt/sources.list.d/kubernetes.list'
sudo apt-get update
sudo apt-get install -y docker.io kubeadm kubelet kubectl kubernetes-cni

cat <<'EOF' | sudo tee /etc/docker/daemon.json
{
  "ipv6": true,
  "fixed-cidr-v6": "2001:db8:1::/64"
}
EOF

cat <<'EOF' | sudo tee /etc/hosts
2001:470:b16e:84::11    istio-ipv6  istio-ipv6.cluster.local
EOF

sudo bash -c 'cat << EOT >> /etc/sysctl.conf
net.ipv6.conf.all.forwarding=1
net.bridge.bridge-nf-call-ip6tables=1
net.bridge.bridge-nf-call-iptables=1
net.ipv6.conf.all.disable_ipv6 = 0
net.ipv6.conf.default.disable_ipv6 = 0
net.ipv6.conf.lo.disable_ipv6 = 0
EOT'
sudo sysctl -p /etc/sysctl.conf

sudo bash -c 'echo 1 > /proc/sys/net/netfilter/nf_log_all_netns'
sudo modprobe br_netfilter || true
sudo bash -c 'echo 1 > /proc/sys/net/bridge/bridge-nf-call-iptables'

sudo systemctl daemon-reload
sudo systemctl start docker
sudo systemctl restart kubelet

#
# Getting interface to bind new ipv6 address and set up a default ipv6 route
#
intf=$(ip addr | awk '/state UP/ {print $2}' | sed 's/://')
sudo ip -6 addr add 2001:470:b16e:84::11 dev "$intf" || true
sudo ip -6 route add ::/0  dev "$intf"  || true

sudo kubeadm init --config ./tools/ipv6/config-ipv6.yaml | tee /tmp/kubeout

# Check if client config has been generated
if [ ! -f "/etc/kubernetes/admin.conf" ]; then
    echo "kubeconfig file not found, exiting..."
    exit 1
fi

sudo cp /etc/kubernetes/admin.conf /tmp/kube.conf
sudo chmod ugo+r /tmp/kube.conf
export KUBECONFIG=/tmp/kube.conf
#
# Wait for control plane to compe up completely, timeout 180 seconds
#
control_plane=("etcd" "kube-apiserver" "kube-controller-manager" "kube-scheduler")
wait_for_control_plane "${control_plane[@]}"

kubectl get pods -n kube-system
#
# Installing calico networking 
#
kubectl create -f ./tools/ipv6/calico.yaml
network_plane=("calico-node" "calico-kube-controllers")
wait_for_control_plane "${network_plane[@]}"

#
# Untain the node in order to run workloads
#
kubectl taint nodes --all=true node-role.kubernetes.io/master:NoSchedule- || true

#
# Collect info from the cluster
#
kubectl get pod --all-namespaces -o wide || true
kubectl get svc --all-namespaces -o wide || true
