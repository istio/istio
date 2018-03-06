CRD File Monitor
======================

This package provides the ability to populate a `crd.Controller` from **yaml** files on disk. The monitor will watch
for changes to yaml files under a specified directory and will update a `crd.Controller` when appropriate.


# Creating a Monitor
To create a monitor, you just need to provide the `crd.Controller`, the directory to be monitored, and the config
descriptor.

```golang
monitor := file.NewMonitor(
    controller,      // The crd controller
    fsroot,          // The root dir to monitor for yaml files
    descriptor)      // Config descriptor (optional)
```

# Running a Monitor
Once created, you simply run the monitor, providing a stop channel.

```golang
stop := make(chan struct{})
...
monitor.Start(stop)
```

The `Start` method will kick off an asynchronous polling loop and will return immediately.
# Example
To configure and run a file monitor with an in-memory config store:

```golang
// Configure the config store and the monitor
store := memory.Make(model.IstioConfigTypes)
controller := memory.NewController(store)
monitor := file.NewMonitor(controller, fsroot, model.IstioConfigTypes)

// Run the controller and monitor
stop := make(chan struct{})
go controller.run(stop)
monitor.Start(stop)
```

See `monitor_test.go` for a concrete example.
# Notes
## Always use a Controller
While the API supports any `model.ConfigStore`, it is recommended to always use a `crd.Controller` so that other
system components can be notified of changes via `controller.RegisterEventHandler()`.
## Update Rate
The timer used for polling the files is currently fixed at 500 milliseconds. This may be made configurable in the future
if it's determined to be useful.
## Config Descriptor
If the config descriptor provided to `NewMonitor` is empty, all of the types from `model.IstioConfigTypes` will be
assumed (i.e. everything).
## Start behavior
The `Start` method will immediately check the directory for any yaml files and update the controller appropriately
before returning. This helps to simplify tests that rely on starting in a particular state.

After performing an initial update, the `Start` method then forks an asynchronous polling loop for update/termination.
