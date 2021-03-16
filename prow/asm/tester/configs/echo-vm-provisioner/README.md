# echo_vm_provisioner

* A namespace as defined in the workload group
* Pushes WorkloadGroup to the namespace
* Creates Managed Instance Group with template
* Creates a VM instance, with the echo app installed
* Appends to a test topology file so the framework can utilize the VM

## Config files

Each suite of VMs has a file structure as follows:

```text
vms/
   ├── a/
   │    ├── workloadgroup.yaml
   │    └── echo.service
   └── b/
        └── ...
```

In this example:

* `vms` is the suite.
* `a` and `b` are the VMs that make up the suite.

There should be one suite for each test job that requires a unique set of VMs/ports.

## workloadgroup.yaml

A valid Istio `WorkloadGroup` that has both `name` and `namespace` defined. Should generally be
annotated with `security.cloud.google.com/IdentityProvider: google` for the ASM usecase.

## echo.service

A valid [systemd unit file](https://man7.org/linux/man-pages/man5/systemd.service.5.html) describing a service named
"echo". This file can be used to configure ports or other customizations to the echo app.
