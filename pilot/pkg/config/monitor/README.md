# Monitor

This package provides the ability to populate a `crd.Controller` dynamically via a function that provides
additional config. The monitor will acquire snapshots of additional changes and populate the `crd.Controller`
as needed.

## Creating a Monitor

To create a monitor, you should provide the `crd.Controller`, a polling interval, and
a function that returns `[]*model.Config`.

```golang
monitor := file.NewMonitor(
    controller,      // The crd controller holding the store and event handlers
    1*time.Second,   // How quickly the monitor requests new snapshots
    getSnapshotFunc) // The function used to acquire new config
```

## Running a Monitor

Once created, you simply run the monitor, providing a stop channel.

```golang
stop := make(chan struct{})
...
monitor.Start(stop)
```

The `Start` method will kick off an asynchronous polling loop and will return immediately.

## Example

To configure and run a monitor that watches for file changes to update an in-memory config store:

```golang
// Configure the config store
store := memory.Make(configDescriptor)
controller = memory.NewController(store)
// Create an object that will take snapshots of config
fileSnapshot := configmonitor.NewFileSnapshot(args.Config.FileDir, configDescriptor)
// Provide snapshot func to monitor
fileMonitor := configmonitor.NewMonitor(controller, 100*time.Millisecond, fileSnapshot.ReadFile)

// Run the controller and monitor
stop := make(chan struct{})
go controller.run(stop)
monitor.Start(stop)
```

See `monitor_test.go` and `file_snapshot_test.go` for more examples.

## Notes

### Always use a Controller

While the API supports any `model.ConfigStore`, it is recommended to always use a `crd.Controller` so that other
system components can be notified of changes via `controller.RegisterEventHandler()`.

### Start behavior

The `Start` method will immediately check the provided `getSnapshotFunc` and update the controller appropriately
before returning. This helps to simplify tests that rely on starting in a particular state.

After performing an initial update, the `Start` method then forks an asynchronous polling loop for update/termination.
