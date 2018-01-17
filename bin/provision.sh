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
# Vim & Curl:
apt-get install -y vim curl

# For NFS speedup:
apt-get install -y nfs-common portmap

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
cd $HOME/golang/src/istio.io
git clone https://github.com/istio/istio.git
chown -R vagrant:vagrant /home/vagrant/golang
echo -e "\nGo $VERSION was installed.\nMake sure to relogin into your shell or run:"
echo -e "\n\tsource $HOME/.${shell_profile}\n\nto update your environment variables."
echo "Tip: Opening a new terminal window usually just works. :)"
rm -f /tmp/go.tar.gz