# Istio installation

This directory contains the default Istio installation configuration in several
different flavors. Also contained is the script for updating it.
 
## updateVersion.sh

The [updateVersion.sh](updateVersion.sh) script is used to update image versions in
[../istio.VERSION](../istio.VERSION) and the istio installation yaml files.

### Options

* `-p <hub>,<tag>` new pilot image
* `-x <hub>,<tag>` new mixer image
* `-c <hub>,<tag>` new citadel image
* `-g <hub>,<tag>` new galley image
* `-a <hub>,<tag>` specifies same hub and tag for pilot, mixer, proxy, citadel and galley containers
* `-o <hub>,<tag>` new proxy image
* `-n <namespace>` namespace in which to install Istio control plane components (defaults to istio-system)
* `-P` URL to download pilot debian packages
* `-d <dir>` directory to store updated/generated files (optional, defaults to source code tree)

Default values for the `-p`, `-x`, `-c`, `-o`, `-g` and `-a` options are as specified in `istio.VERSION`
(i.e., they are left unchanged).

### Examples

Update the pilot and istioctl:

```
./updateVersion.sh -p "docker.io/istio,2017-05-09-06.14.22"
```
