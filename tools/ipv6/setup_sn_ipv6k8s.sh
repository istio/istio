#!/bin/bash -e

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

sudo ip -6 addr add 2001:470:b16e:84::11 dev docker0 || true
sudo kubeadm init --config config-ipv6.yaml | tee /tmp/kubeout

# Check if client config has been generated
if [ ! -f "/etc/kubernetes/admin.conf" ]; then
    echo "kubeconfig file not found, exiting..."
    exit -1
fi

sudo cp /etc/kubernetes/admin.conf /tmp/kube.conf
sudo chmod ugo+r /tmp/kube.conf
export KUBECONFIG=/tmp/kube.conf
#
# Wait for control plane to compe up completely, timeout 180 seconds
#
./wait_for_control_plane.sh

kubectl get pods -n kube-system
