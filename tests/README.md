# Integration Testing

This directory contains scripts for running a full Istio end-to-end integration test (bookinfo).
 
## kubeTest.sh

The [kubeTest.sh](kubeTest.sh) script allows a user to run the integration test using
any combination of `istioctl`, `pilot`, and `mixer` versions. Developers should use
it to test their changes before creating PRs.

### Options

* `-p <hub>,<tag>` specify a pilot image to use
* `-x <hub>,<tag>` specify a mixer image to use
* `-i <url>` specify an `istioctl` download URL
* `-c <istioctl>` the location of an `istioctl` binary
* `-n <namespace>` run the test in the specified namespace
* `-s` don't shutdown and cleanup after running the test

Default values for the `-m`, `-x`, and `-i` options are as specified in
[istio.VERSION](../istio.VERSION).
The `-c` option, if specified, overrides the `-i` value.

### Examples

Test a particular pilot image (for example, after a successful run of `pilot/bin/e2e.sh`):

```
./kubeTest.sh -m "docker.io/myaccount,ubuntu_20170404_151557"
```

Test a particular mixer image:

```
./kubeTest.sh -x "gcr.io/istio-testing,2017-04-06-18.08.24"
```

Test an arbitrary explicit configuration:

```
./kubeTest.sh -s -n test-namespace \
  -m "gcr.io/istio-testing,alpha9a73dd7feb916a7af889b94558b579ccee261a26" \
  -x "gcr.io/istio-testing,2017-04-04-22.14.44" \
  -c ~/go/src/istio.io/pilot/istioctl-linux
```

## Developer process 

1. Run `kubeTest.sh -m "<pilot hub>,<pilot tag>"`, `kubeTest.sh -x "<mixer hub>,<mixer tag>"`,
   or `kubeTest.sh -c "<istioctl binary>"` to test your changes to pilot, mixer, 
   or istioctl, respectively. 
2. Submit a PR with your changes to `istio/pilot` or `istio/mixer`.
3. Run `../install/updateVersion.sh` to update the default Istio install configuration and then
   submit a PR  to `istio/istio` for the version change.
   
   >>> Note: in the future step 3 will be done by the Jenkins build automatically
   >>> whenever a new pilot or mixer is successfully built.
