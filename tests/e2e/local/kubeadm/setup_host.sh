#!/bin/bash

echo "Starting kubeadm."
# In order for Network Policy to work correctly, you need to pass --pod-network-cidr=192.168.0.0/16 to kubeadm init.
kubeadm init --pod-network-cidr=192.168.0.0/16  --node-name=e2e-test-local

mkdir -p "$HOME/.kube"
sudo cp -i /etc/kubernetes/admin.conf "$HOME/.kube/config"
sudo chown "$(id -u)":"$(id -g)" "$HOME/.kube/config"

# By default, cluster created by kubeadm will not schedule pods on the master.  If we want to be able to
# schedule pods on the master, e.g. for a single-machine Kubernetes cluster for development, run:
kubectl taint nodes --all node-role.kubernetes.io/master-

# Install Calico. Note that Calico works on amd64 only.
kubectl apply -f https://docs.projectcalico.org/v3.1/getting-started/kubernetes/installation/hosted/rbac-kdd.yaml
kubectl apply -f https://docs.projectcalico.org/v3.1/getting-started/kubernetes/installation/hosted/kubernetes-datastore/calico-networking/1.7/calico.yaml

while ! kubectl get pods -n kube-system | grep coredns |  grep Running > /dev/null; do
  echo "coredns not ready, will check again in 5 sec"
  sleep 5
done

# Set up env ISTIO if not done yet
if [[ -z "${ISTIO// }" ]]; then
  if [[ -z "${GOPATH// }" ]]; then
    echo GOPATH is not set. Please set and run script again.
    exit
  fi
  export ISTIO=$GOPATH/src/istio.io
  echo 'Set ISTIO to' "$ISTIO"
fi

echo "Host Setup Completed"
