#!/bin/bash

# Restore kubectl 
cp ~/.kube/config_old ~/.kube/config
rm -rf ~/.kube/config_old