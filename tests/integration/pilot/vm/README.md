
# Writing Tests for VMs

This README serves as a documentation for those who would continue writing and extending tests for VMs.
**Note: We currently use mock/simulated VMs for testing purposes. In the future, the testing might switch
to utilize actual compute instances from different providers.**

## Deploy an echo instance as a VM

In most cases, deploying an echo instance as a VM is as simple as setting `DeployAsVm` to be true in `echo.Config`
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
the comments. To make it easier in most cases, we have a const variable called `DefaultVMImage` to help maintenance
and reference. More utility functions and variables could be added to the util file. Currently, we set `DefaultVMImage`
to be `ubuntu:bionic`, but it is subject to change. If `VMImage` is not
provided while `DeployAsVM` is on, it will default the Docker image to be `DefaultVMImage`. For pre-submit jobs, the
only image available would be `ubuntu:bionic`, and all other images would be built in post-submit jobs. More information
could be found in the next section.

## Adding Docker images
Current, we test the VMs with `ubuntu:xenial`, `ubuntu:bionic`, `ubuntu:focal`, `debian:9` and `debian:10`. The images
will be created from [prow/lib.sh](https://github.com/istio/istio/blob/master/prow/lib.sh). We will only build
`ubuntu:bionic` for pre-submit jobs and build all others for post-submit jobs only to save time for CI/CD. To add more
images for testing, simply modify [tools/istio-docker.mk](https://github.com/istio/istio/blob/master/tools/istio-docker.mk)
to add more targets. `prow/lib.sh` would have to be updated as well to build the images in prow. If you are writing
a test that would need multiple images, the test needs to be labeled as `PostSubmit`, or it will fail since the
images won't be built in pre-submit jobs.

## DNS Resolution
Currently, VMs resolving DNS does not happen automatically. During the build stage of the Docker images, we manually
edit the `/etc/hosts` for the VMs to send requests to the pods in the cluster as shown [here](https://github.com/istio/istio/blob/9eff11ae3c6271ee06ee6d4d57a22a33749614a4/pkg/test/framework/components/echo/kube/deployment.go#L233-L234).
Until we have a better way of resolving DNS, this remains a workaround for the VMs to connect to the cluster and `Istiod`.

