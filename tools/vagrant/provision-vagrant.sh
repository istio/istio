#!/bin/bash
set -e
VERSION="1.9.2"

# Update, get python-software-properties in order to get add-apt-repository, 
# then update (for latest git version):
apt-get update
apt-get install -y python-software-properties
add-apt-repository -y ppa:git-core/ppa
apt-get update
apt-get install -y git
apt-get install -y make
apt-get install -y docker 
# Vim & Curl:
apt-get install -y vim curl

# Install golang 
shell_profile="bashrc"
DFILE="go$VERSION.linux-amd64.tar.gz"
HOME="/home/vagrant"
echo "Downloading $DFILE ..."
wget https://dl.google.com/go/$DFILE -O /tmp/go.tar.gz

if [ $? -ne 0 ]; then
    echo "Download failed! Exiting."
    exit 1
fi

echo "Extracting File..."
tar -C "$HOME" -xzf /tmp/go.tar.gz
mv "$HOME/go" "$HOME/.go"

touch "$HOME/.${shell_profile}"
{
    echo '# GoLang'
    echo 'export GOROOT=$HOME/.go'
    echo 'export PATH=$PATH:$GOROOT/bin'
    echo 'export GOPATH=$HOME/golang'
    echo 'export PATH=$PATH:$GOPATH/bin'
} >> "$HOME/.${shell_profile}"

mkdir -p $HOME/golang/{src,pkg,bin}
mkdir -p $HOME/golang/src/istio.io

chown -R vagrant:vagrant /home/vagrant/golang
echo -e "\nGo $VERSION was installed.\nMake sure to relogin into your shell or run:"
echo -e "\n\tsource $HOME/.${shell_profile}\n\nto update your environment variables."
rm -f /tmp/go.tar.gz

# install minikube
export K8S_VER=v1.7.4
export MASTER_IP=127.0.0.1
export MASTER_CLUSTER_IP=10.99.0.1
mkdir -p /tmp/apiserver && \
cd /tmp/apiserver && \
wget https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kube-apiserver && \
chmod +x /tmp/apiserver/kube-apiserver

cd /tmp && \
curl -L -O https://storage.googleapis.com/kubernetes-release/easy-rsa/easy-rsa.tar.gz && \
tar xzf easy-rsa.tar.gz && \
cd easy-rsa-master/easyrsa3 && \
./easyrsa init-pki && \
./easyrsa --batch "--req-cn=${MASTER_IP}@`date +%s`" build-ca nopass && \
./easyrsa --subject-alt-name="IP:${MASTER_IP},""IP:${MASTER_CLUSTER_IP},""DNS:kubernetes,""DNS:kubernetes.default,""DNS:kubernetes.default.svc,""DNS:kubernetes.default.svc.cluster,""DNS:kubernetes.default.svc.cluster.local" --days=10000 build-server-full server nopass && \
cp /tmp/easy-rsa-master/easyrsa3/pki/ca.crt /tmp/apiserver/ca.crt && \
cp /tmp/easy-rsa-master/easyrsa3/pki/issued/server.crt /tmp/apiserver/server.crt && \
cp /tmp/easy-rsa-master/easyrsa3/pki/private/server.key /tmp/apiserver/server.key && \
cd /tmp && \
rm -rf /tmp/easy-rsa-master/
  
# Include minikube and kubectl in the image
curl -Lo /tmp/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kubectl && \
chmod +x /tmp/kubectl && sudo mv /tmp/kubectl /usr/local/bin/

curl -Lo /tmp/minikube https://storage.googleapis.com/minikube/releases/v0.22.3/minikube-linux-amd64 &&\
chmod +x /tmp/minikube && sudo mv /tmp/minikube /usr/local/bin/
