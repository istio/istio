#!/bin/bash

# Check if homebrew is installed
if ! brew --help > /dev/null; then
    echo "Homebrew is not installed. Please go to https://docs.brew.sh/Installation to install Homebrew."
    exit 1
fi

echo "Update homebrew..."
brew update > /dev/null
# Give write permissions for /usr/local/bin, so that brew can create symlinks.
sudo chown -R $USER:admin /usr/local/bin

echo "Checking curl"
if ! curl --help > /dev/null; then
    echo "curl is not installed. Install it from homebrew."
    if ! brew install curl; then
    	echo "Installation of curl from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "curl exists."
fi

echo "Checking docker..."
if ! docker --help > /dev/null; then
    echo "docker is not installed. Install it from homebrew cask."
    if ! brew cask install docker; then
    	echo "Installation of docker from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "docker exists. Please make sure to update it to latest version."
fi

echo "Checking docker-machine..."
if ! docker-machine --help > /dev/null; then
    echo "docker-machine is not installed. Downloading and Installing it using curl."
    base=https://github.com/docker/machine/releases/download/v0.14.0 &&
    if ! curl -L "$base/docker-machine-$(uname -s)-$(uname -m)" >/usr/local/bin/docker-machine && chmod +x /usr/local/bin/docker-machine; then
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
    # shellcheck disable=SC2181
    if [ $? -ne 0 ]; then
        echo "Installation of hyperkit driver failed. Please install it manually."
        exit 1
    fi
}

echo "Checking hyperkit..."
if ! hyperkit -h > /dev/null; then
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

# Install minikube.
function install_minikube() {
    echo "Minikube version 0.27.0 is not installed. Installing it using curl."
    if ! curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.27.0/minikube-darwin-amd64 \
            && chmod +x minikube \
            && sudo mv minikube /usr/local/bin/; then
        echo "Installation of Minikube version 0.27.0 failed. Please install it manually."
        exit 1
    else
        echo "Done."
    fi
}

echo "Checking and Installing Minikube version 0.27.0 as required"

# If minikube is installed.
if minikube --help > /dev/null; then
# If version is not 0.27.0.
if [[ $(minikube version) != *"minikube version: v0.27.0"* ]]; then
    # Uninstall minikube.
    echo "Deleting previous minikube cluster and updating minikube to v0.27.0"
    minikube delete

    install_minikube
fi
else
    install_minikube
fi

echo "Checking kubectl..."
if ! kubectl --help > /dev/null; then
    echo "kubectl is not installed. Installing the lastest stable release..."
    if ! brew install kubectl; then
    	echo "Installation of kubectl from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "kubectl exists. Please make sure to update it to latest version."
fi

echo "Prerequisite check and installation process finishes."
