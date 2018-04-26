#!/bin/bash

# Setting up kubectl on host to talk to kubernetest cluster on Vagrant VM.
echo "your old ~/.kube/config file can be found at ~/.kube/config_old"
ls ~/.kube/config_old
if [ $? -ne 0 ]; then
    cp ~/.kube/config ~/.kube/config_old
else
    echo "There is an old ~/.kube/config_old file on your system."
    read -p "If you believe it's outdated, we can update it[default: no]: " update
    overrwriteExisting=${update:-"no"}
    if [[ $overrwriteExisting = *"y"* ]] || [[ $overrwriteExisting = *"Y"* ]]; then
        cp ~/.kube/config ~/.kube/config_old
    fi
fi
vagrant ssh -c "cat ~/.kube/config" > ~/.kube/config
