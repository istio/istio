# Istio installation

This directory contains the default Istio installation configuration and
the script for updating it.
 
## updateVersion.sh

The [updateVersion.sh](updateVersion.sh) script is used to update image versions in
[../istio.VERSION](../istio.VERSION) and the istio installation yaml files.

### Options

* `-p <hub>,<tag>` new pilot image
* `-x <hub>,<tag>` new mixer image
* `-c <hub>,<tag>` new ca image
* `-i <url>` new `istioctl` download URL
* `-g` create a `git commit` titled "Updating istio version" for the changes
* `-n` <namespace> namespace in which to install Istio control plane components (defaults to "istio-system")

Default values for the `-p`, `-x`, `-c`, and `-i` options are as specified in `istio.VERSION`
(i.e., they are left unchanged).

### Examples

Update the pilot and istioctl:

```
./updateVersion.sh -p "docker.io/istio,2017-05-09-06.14.22" -c "https://storage.googleapis.com/istio-artifacts/dbcc933388561cdf06cbe6d6e1076b410e4433e0/artifacts/istioctl"
```
