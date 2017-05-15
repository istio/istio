# Integration Testing

This directory contains scripts for running a full Istio end-to-end integration test (bookinfo)
and for updating the default Istio installation configuration.

## e2e.sh

The [e2e.sh](e2e.sh) script `source`s the version inforation in
[istio.VERSION](../istio.VERSION) then
uses [Bazel](https://bazel.build/) to execute
Go tests defined in the _BUILD_ files.

### Environment variables

* BAZEL_STARTUP_ARGS
* BAZEL_RUN_ARGS

## kubeTest.sh

The [kubeTest.sh](kubeTest.sh) script allows a user to run the integration test using
any combination of `istioctl`, `manager`, and `mixer` versions. Developers should use
it to test their changes before creating PRs.

### Options

* `-m <hub>,<tag>` specify a manager image to use
* `-x <hub>,<tag>` specify a mixer image to use
* `-i <url>` specify an `istioctl` download URL
* `-c <istioctl>` the location of an `istioctl` binary
* `-n <namespace>` run the test in the specified namespace
* `-s` don't shutdown and cleanup after running the test

Default values for the `-m`, `-x`, and `-i` options are as specified in
[istio.VERSION](../istio.VERSION).
The `-c` option, if specified, overrides the `-i` value.

### Examples

Test a particular manager image (for example, after a successful run of `manager/bin/e2e.sh`):

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
  -c ~/go/src/istio.io/manager/istioctl-linux
```

## updateVersion.sh

The [updateVersion.sh](../updateVersion.sh) script is used to generate istio yaml installation files based on 
the templates from [../install/kubernetes/templatest](../install/kubernetes/templates) and the image tags specified
in [istio.VERSION](../istio.VERSION).

### Options

* `-m <hub>,<tag>` new manager image
* `-x <hub>,<tag>` new mixer image
* `-i <url>` new `istioctl` download URL
* `-c` create a `git commit` titled "Updating istio version" for the changes

Default values for the `-m`, `-x`, and `-i` options are as specified in `istio.VERSION`
(i.e., they are left unchanged).

## Developer process 

1. Run `kubeTest.sh -m "<manager hub>,<manager tag>"`, `kubeTest.sh -x "<mixer hub>,<mixer tag>"`,
   or `kubeTest.sh -c "<istioctl binary>"` to test your changes to manager, mixer, 
   or istioctl, respectively. 
2. Submit a PR with your changes to `istio/manager` or `istio/mixer`.
3. Run `updateVersion.sh` to update the default Istio install configuration and then
   submit a PR  to `istio/istio` for the version change.
   
   >>> Note: in the future step 3 will be done by the Jenkins build automatically
   >>> whenever a new manager or mixer is successfully built.
