# Writing Tests for VMs

Support for including VMs in the integration tests has been added to the framework.
This document describes the integration and extension mechanisms to exercise VM related code.
The primary goals are to test VM-related code & ensure it works on a range of supported OS types and versions.

**Note: We currently use mock/simulated VMs for testing purposes. In the future, the testing might switch
to utilize actual compute instances from different providers.**

## Overview

This section describes the scenarios in which one might want to add a VM test:
1. Testing existing core Istio features such as traffic management, security, telemetry, etc. for **VMs**
2. Supporting new OS images for VMs
3. Testing onboarding tools (iptables, certs, istio-sidecar, etc.) and workflows (services, DNS, etc.) to enmesh a VM

## Secenario 1: Testing VM-related Istio Code

We normally test Istio using Echo application. To test VMs' connectivity, security and telemetry,
we deploy a regular Echo instance as VM. A VM Echo instance will simulate a VM, disabling kube-dns,
Service Account mount, etc. For more information around VM onboarding,
refer to this [doc](https://istio.io/latest/docs/examples/virtual-machines/single-network/).

To deploy an echo instance as a VM
1. Set the ports for the VMs. Note that due to a bug in WorkloadEntry,
we need to set the ServicePort to be the same as the InstancePort
2. Set `DeployAsVm` to be true in `echo.Config`
3. Set `VMImage` to be any image available from `GetSupportedOSVersion` in [util.go](https://github.com/istio/istio/blob/master/tests/integration/pilot/vm/util.go).

For example,

```go
ports := []echo.Port{
   {
       Name:     "http",
       Protocol: protocol.HTTP,
       // Due to a bug in WorkloadEntry, service port must equal target port for now
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
   VMImage:    "app_sidecar_ubuntu_bionic"
}
```

There is a default image and one could reference it with `DefaultVMImage` so that it's easier to maintain
if the supported OSes were to change. We have [additional images](https://github.com/istio/istio/blob/master/tests/integration/pilot/vm/util.go)
that are built in the post-submit jobs, jobs that only run after a PR is merged.
Info regarding adding extra images could be found in the next section.
If `VMImage` is not provided while `DeployAsVM` is on, it will default the Docker image to be `DefaultVMImage`.

## Scenario 2: Supporting More OS Images

We list the supported OSes in [util.go](https://github.com/istio/istio/blob/master/tests/integration/pilot/vm/util.go)
and the images will be created in [prow/lib.sh](https://github.com/istio/istio/blob/master/prow/lib.sh).

To add additional supported images for testing:
1. Modify [tools/istio-docker.mk](https://github.com/istio/istio/blob/master/tools/istio-docker.mk) to add more
build targets. See other build targets for references. Basically, one needs to specify the OS image name and version,
and pass it to the Dockerfile.
2. Modify `prow/lib.sh` by adding images to the targets so that the image could be built on fly for CI/CD.
3. Add the images to `util.go` to be tested in PostSubmit jobs.
4. (Optional) Modify the `DefaultVMImage` in `util.go` in case the default supported image changes.

**Note: We will only build `ubuntu:bionic` (default) for pre-submit jobs and build all others for post-submit jobs
to save time in CI/CD. If you are writing a test that would need multiple images, the test
needs to be labeled as `PostSubmit`, or it will fail since the images other than default won't be built
in pre-submit jobs.**

## Scenario 3: Testing VM Onboarding & Enmeshing

Detailed steps to onboard a VM could be found in this [documentation](https://istio.io/latest/docs/examples/virtual-machines/single-network/) for the VM onboarding process.

In a nutshell, steps for the VM to participate in the mesh involves:
1. Install the certificates
2. Edit `/etc/hosts` for the VM to know the IP addresss of `Istiod`
3. Start the istio-sidecar.deb file
4. Generate workload entries for the VM

Currently, these steps are pre-configured and built in the deployment. However, each of them could be tested
by tweaking the [VM deployment template](https://github.com/istio/istio/blob/master/pkg/test/framework/components/echo/kube/deployment.go#L193).

