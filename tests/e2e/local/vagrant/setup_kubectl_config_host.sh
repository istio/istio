#!/bin/bash

# Setting up kubectl on host to talk to kubernetest cluster on Vagrant VM.

# Save and unset KUBECONFIG in case users set it pointing to a k8s cluster
export KUBECONFIG_SAVED=$KUBECONFIG
unset KUBECONFIG

# Set kube config file on host
if ! ls ~/.kube/config_old > /dev/null; then
    if ls ~/.kube/config > /dev/null; then
    	cp ~/.kube/config ~/.kube/config_old
    	echo "your old ~/.kube/config file can be found at ~/.kube/config_old"
    fi
else
    echo "There is an old ~/.kube/config_old file on your system."
    read -p "If you believe it's outdated, we can update it[default: no]: " update
    overrwriteExisting=${update:-"no"}
    if [[ $overrwriteExisting = *"y"* ]] || [[ $overrwriteExisting = *"Y"* ]]; then
        cp ~/.kube/config ~/.kube/config_old
    fi
fi
vagrant ssh -c "cat ~/.kube/config" > ~/.kube/config

echo "kubectl setup done."
