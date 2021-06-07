# Writing Tests for VMs

This document describes the integration and extension mechanisms to exercise VM related code.
The primary goals are to:
1. Test VM-related Istio code so that newly submitted commits won't break VM support
1. Ensure the code works on a range of supported OS types and versions.

**Note: We currently use mock/simulated VMs for testing purposes. In the future, the testing might switch
to utilize actual compute instances from different providers.**

## Overview

Scenarios in which one might want to add a VM test in this doc:
1. Testing existing core Istio features such as traffic management, security, telemetry, etc. for **VMs**
1. Supporting new OS images for VMs
1. Testing onboarding tools (iptables, certs, istio-sidecar, etc.) and workflows (services, DNS, etc.) to enmesh a VM

## Secenario 1: Testing VM-related Istio Code

Most integration tests in Istio use the Echo application. To test connectivity, security and telemetry for a VM
in the mesh, we deploy an instance of the Echo application as a VM resource. A VM Echo instance will simulate a VM,
disabling kube-dns, Service Account mount, etc. For more information around VM onboarding,
refer to this [doc](https://istio.io/latest/docs/setup/install/virtual-machine/).

To deploy an echo instance as a VM
1. Set the ports for the VMs.
1. Set `DeployAsVm` to be true in `echo.Config`.
We used DefaultVMImage in the example.

For example,

```go
ports := []echo.Port{
   {
       Name:     "http",
       Protocol: protocol.HTTP,
       InstancePort: 8090,
       ServicePort:  8090,
   },
}
echo.Config{
   Service:    "vm",
   Namespace:  "virtual-machine",
   Ports:      ports,
   Pilot:      p,
   DeployAsVM: true,
   VMImage:    vm.DefaultVMImage
}
```

The default image referenced with `DefaultVMImage` from `vm` package should be used for all pre-submit tests since
this is the only image available in the pre-submit stage. Using additional images are only possible in
post-submit tests as shown in the [next section](#scenario-2-supporting-additional-os-images ).
A complete list of supported additional images can be found in [`vm_test.go`](https://github.com/istio/istio/blob/master/tests/integration/pilot/vm_test.go).
If `VMImage` is not provided while `DeployAsVM` is on, it will default the Docker image to be `DefaultVMImage`.

## Scenario 2: Supporting Additional OS Images

We list the supported OSes in [vm_test.go](https://github.com/istio/istio/blob/master/tests/integration/pilot/vm_test.go)
and the images will be created in [prow/lib.sh](https://github.com/istio/istio/blob/master/prow/lib.sh).

To add additional supported images for testing:
1. Modify [tools/istio-docker.mk](https://github.com/istio/istio/blob/master/tools/istio-docker.mk) to add more
build targets. Specify the OS image name and version in the Makefile, and it will be passed to the Dockerfile.
See other build targets for references.
1. Modify `prow/lib.sh` by adding images to the targets to build images for CI/CD.
1. Add the images to `util.go` to be tested in PostSubmit jobs.
1. (Optional) Modify the `DefaultVMImage` in `util.go` in case the default supported image changes.

**Note: We will only build default image for pre-submit jobs and build all others for post-submit jobs
to save time in CI/CD.**

## Scenario 3: Testing VM Onboarding & Enmeshing

Detailed steps to onboard a VM could be found in [VM onboarding documentation](https://istio.io/latest/docs/setup/install/virtual-machine/).

Currently, these steps are pre-configured and built in the deployment. However, each of them could be tested
by tweaking the [VM deployment template](https://github.com/istio/istio/blob/master/pkg/test/framework/components/echo/kube/deployment.go#L193).

