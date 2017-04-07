# Integration Testing

This directory contains scripts for running a full Istio end-to-end integration test (bookinfo)
and for updating the default Istio installation configuration.
 
## kubeTest.sh

The [kubeTest.sh](kubeTest.sh) script allows a user to run the integration test using
any combination of `istioctl`, `manager`, and `mixer` versions. Developers should use
it to test their changes before creating PRs.

### Options

* `-m <hub>,<tag>` specify a manager image to use
* `-x <hub>,<tag>` specify a mixer image to use
* `-i <istioctl>` the location of a suitable `istioctl` binary
* `-n <namespace>` run the test in the specified namespace
* `-s` don't shutdown and cleanup after running the test

Default values for the `-m`, `-x`, and `-i` options are as specified in
[istio.VERSION](../istio.VERSION).

### Examples

Test a particular manager image (for example, after a successful run of `manager/bin/e2e.sh`):

```
./kubeTest.sh -m "docker.io/myaccount,ubuntu_20170404_151557"
```

Test a particular mixer image:

```
./kubeTest.sh -x "gcr.io/istio-testing/mixer:2017-04-06-18.08.24"
```

Test an arbitrary explicit configuration:

```
./kubeTest.sh -s -n test-namespace \
  -m "gcr.io/istio-testing,alpha9a73dd7feb916a7af889b94558b579ccee261a26" \
  -x "gcr.io/istio-testing,2017-04-04-22.14.44" \
  -i ~/go/src/istio.io/manager/istioctl-linux
```

## update_version.sh

The [update_version.sh](../scripts/update_version.sh) script is used to update 
the images specified in [istio.VERSION](../istio.VERSION) and the default Istio
installation in [../kubernetes/istio-install](../kubernetes/istio-install).
The resulting configuration is then committed to the `master` branch of `istio/istio`.
   
### Options

* `-m <hub>,<tag>` new manager image
* `-x <hub>,<tag>` new mixer image
* `-i <istioctl>` new `istioctl` binary

Default values for the `-m`, `-x`, and `-i` options are as specified in `istio.VERSION`
(i.e., unchanged).

## Developer process 

1. Run `kubeTest.sh -m "<manager hub>,<manager tag>` or `kubeTest.sh -x "<mixer hub>,<mixer tag>` 
   to test your changes. 
2. Submit a PR with your changes to `istio/manager` or `istio/mixer`.
3. Run `update_version.sh` to update the default Istio install configuration in `istio/istio`.
   
   >>> Note: in the future step 3 will be done by the Jenkins build automatically
   >>> whenever a new manager or mixer is successfully built.
