# Upgrade testdata

This folder contains a snapshot of older released versions of Istio yamls to install a base version of
Istio as well as a custom gateway.

## Adding versions

To add a version, download the release and run

`./bin/istioctl manifest generate -f .customgw.yaml > <version>-cgw-install.yaml`

and

`./bin/istioctl manifest generate -f base.yaml > <version>-base-install.yaml`.

tar up the files like

`tar cf <version>-<name>-yaml.tar <version>-<name>-yaml`

and place them in this directory.
