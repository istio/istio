#!/bin/bash

rm -rf ~/.kube/config

# Restore kubectl
if ls ~/.kube/config_old > /dev/null; then
	cp ~/.kube/config_old ~/.kube/config
	rm -rf ~/.kube/config_old
	echo "Your old kube config file has been restored."
fi

# Restore KUBECONFIG if existed
export KUBECONFIG=$KUBECONFIG_SAVED