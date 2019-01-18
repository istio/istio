## How to add a new plugin:

* Add a new subdirectory.

* Export GetInfo() that conforms to authplugin.InfoFn

* In the returned authplugin.Info:

  * Set `Name` to what will be used to specify your plugin in config
  * Set `GetAuth` to a function in your plugin that conforms to authplugin.AuthFn

* Edit inventory.go in this directory so that your plugin's GetInfo is
  retuned in the slice that Inventory() returns.
