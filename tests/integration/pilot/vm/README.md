# Writing Tests for VMs

Support for including VMs in the integration tests has been added to the framework. 
This document describes the integration and extension mechanisms to exercise VM related code. 
The primary goals are to test VM-related code & ensure it works on a range of supported OS types and versions.  

**Note: We currently use mock/simulated VMs for testing purposes. In the future, the testing might switch
to utilize actual compute instances from different providers.**

## Deploy an Echo Instance as a VM

To deploy an echo instance as a VM just set `DeployAsVm` to be true in `echo.Config`
and `VMImage` to be any image available from `GetSupportedOSVersion`. For example,
```
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
Note that due to a bug in WorkloadEntry, we need to set the ServicePort to be the same as the InstancePort, as shown in
the comments. There is a default image and one could reference it with `DefaultVMImage` so that it's easier to maintain
if the supported OSes were to change. We have [additional images](https://github.com/istio/istio/blob/d89c6a624cfd0962e2ed25238ec671baebda0ca5/tests/integration/pilot/vm/util.go#L23) 
that are built in the post-submit jobs, jobs that only run after a PR is merged. 
Info regarding adding extra images could be found in the next section. 
If `VMImage` is not provided while `DeployAsVM` is on, it will default the Docker image to be `DefaultVMImage`.

## Adding Docker Images
We list the supported OSes in [util.go](https://github.com/istio/istio/blob/d89c6a624cfd0962e2ed25238ec671baebda0ca5/tests/integration/pilot/vm/util.go#L23) 
and the images will be created in [prow/lib.sh](https://github.com/istio/istio/blob/master/prow/lib.sh). 
We will only build `ubuntu:bionic` for pre-submit jobs and build all others for post-submit jobs only to save time for CI/CD. 
To add more images for testing, simply modify [tools/istio-docker.mk](https://github.com/istio/istio/blob/master/tools/istio-docker.mk)
to add more targets. `prow/lib.sh` would have to be updated as well to build the images on the fly in prow. 
If you are writing a test that would need multiple images, the test needs to be labeled as `PostSubmit`, or it 
will fail since the images other than default won't be built in pre-submit jobs.

## VM Deployment
The deployment of VM involves steps from the [documentation](https://istio.io/latest/docs/examples/virtual-machines/single-network/) for the VM onboarding process. 
We need to specify the IP address of the `Istiod` so that the VM could form a connection to Istio.
During the deployment of the Docker images, we would also manually edit the `/etc/hosts` for the VMs to send requests to the 
pods in the cluster, as shown [here](https://github.com/istio/istio/blob/9eff11ae3c6271ee06ee6d4d57a22a33749614a4/pkg/test/framework/components/echo/kube/deployment.go#L233-L234).
Test cases that require the VM to send requests to other pods would have to add additional lines to the hosts. 
