#!/bin/bash

# Check if homebrew is installed
brew --help > /dev/null
if [ $? -ne 0 ]; then
    echo "Homebrew is not installed. Please go to https://docs.brew.sh/Installation to install Homebrew."
    exit 1
fi

echo "Update homebrew..."
brew update > /dev/null
# Give write permissions for /usr/local/bin, so that brew can create symlinks.
sudo chown -R $USER:admin /usr/local/bin

echo "Checking curl"
curl --help > /dev/null
if [ $? -ne 0 ]; then
    echo "curl is not installed. Install it from homebrew."
    brew install curl
    if [ $? -ne 0 ]; then
    	echo "Installation of curl from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "curl exists."
fi

echo "Checking docker..."
docker --help > /dev/null
if [ $? -ne 0 ]; then
    echo "docker is not installed. Install it from homebrew cask."
    brew cask install docker
    if [ $? -ne 0 ]; then
    	echo "Installation of docker from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "docker exists. Please make sure to update it to latest version."
fi

echo "Checking docker-machine..."
docker-machine --help > /dev/null
if [ $? -ne 0 ]; then
    echo "docker-machine is not installed. Downloading and Installing it using curl."
    base=https://github.com/docker/machine/releases/download/v0.14.0 &&
  curl -L $base/docker-machine-$(uname -s)-$(uname -m) >/usr/local/bin/docker-machine && chmod +x /usr/local/bin/docker-machine
    if [ $? -ne 0 ]; then
        echo "Installation of docker-machine failed. Please install it manually."
        exit 1
    else
        echo "Done."
    fi
else
    echo "docker-machine exists. Please make sure to update it to latest version."
    docker-machine version
fi

function fail_hyperkit_installation() {
    if [ $? -ne 0 ]; then
        echo "Installation of hyperkit driver failed. Please install it manually."
        exit 1
    fi
}

echo "Checking hyperkit..."
hyperkit -h > /dev/null
if [ $? -ne 0 ]; then
    echo "hyperkit is not installed. Downloading and installing using curl."
    curl -LO https://storage.googleapis.com/minikube/releases/latest/docker-machine-driver-hyperkit
    fail_hyperkit_installation
    chmod +x docker-machine-driver-hyperkit
    fail_hyperkit_installation
    sudo mv docker-machine-driver-hyperkit /usr/local/bin/
    fail_hyperkit_installation
    sudo chown root:wheel /usr/local/bin/docker-machine-driver-hyperkit
    fail_hyperkit_installation
    sudo chmod u+s /usr/local/bin/docker-machine-driver-hyperkit
    fail_hyperkit_installation
else
    echo "hyperkit is installed. Install newer version if available."
    hyperkit version
fi

echo "Checking and Installing Minikube version 0.27.0 as required..."
minikube --help > /dev/null
if [[ $? -ne 0 || ($(minikube version) != *"minikube version: v0.27.0"*) ]]; then
    if [ $? -eq 0 ]; then
        echo "Deleting previous minikube cluster and updating minikube to v0.27.0"
        minikube delete
    fi
    echo "Minikube version 0.27.0 is not installed. Installing it using curl."
    curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.27.0/minikube-darwin-amd64 && chmod +x minikube && sudo mv minikube /usr/local/bin/
    if [ $? -ne 0 ]; then
    	echo "Installation of Minikube version 0.27.0 failed. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
fi

echo "Checking kubectl..."
kubectl --help > /dev/null
if [ $? -ne 0 ]; then
    echo "kubectl is not installed. Installing the lastest stable release..."
    brew install kubectl
    if [ $? -ne 0 ]; then
    	echo "Installation of kubectl from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "kubectl exists. Please make sure to update it to latest version."
fi

echo "Prerequisite check and installation process finishes."
