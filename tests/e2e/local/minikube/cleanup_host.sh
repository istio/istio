#!/bin/bash

# Getting docker back to it's previous setup.
eval "$(docker-machine env -u)"

# Removing port forwarding that was setup.
kill `ps -eaf | grep "kubectl port-forward" | awk '{print $2;}'`


