# Istio-wide Integration Test Framework ** DEPRECATED. DO NOT USE. **

This framework provides a universal way to bring various components into a test environment.
Unlike [e2e tests](https://github.com/istio/istio/tree/master/tests/e2e), integration tests usually don't need to start the whole package of istio
and application services, instead, each integration test only needs several modules related to this test case.
This integration test framework is built for this requirement. It's designed to be flexible enough for developers to pick the combination of components based on what behaviors are tested.

## Concept
The idea is to decouple **component** with **test case**.

![integration_framework_structure](https://github.com/istio/istio/tree/master/tests/integration/images/integration_framework_structure.jpg)

We break down `component`, `test environment` and `framework` into different abstractions.

#### Component
`Component` is the smallest element in this framework, it can be any separated modules (mixer, proxy...), applications (bookinfo, echo server) or external tools (prometheus).

#### Environment
`Environment` is the package that combines multiple components as well as some higher level test environment setting.

#### Framework
`TestEnvManager` is the core part of this framework. It's a test environment manager for environment setting up and tearing down. 
It takes one environment, brings up everything defined in this environment including components, kicks off tests with results recorded, and then tears down components and cleans up the environment afterwards.

#### Life cycle of a component:  
* Components are defined in a environment by creating an instance in `GetComponents() []framework.Component` in `test environment`.
* In `TestEnvManager.StartUp()` components will be triggered to start at once, and then `TestEnvManager.WaitUntilReady()` is going to check if everything is ready.
If not, it will wait and retry several times before either the entire environment is ready or some components still fail after multiple attempts.
If the environment can't be ready, the framework will throw a error and exit.
* Components will be stopped in `testEnvManager.TearDown()` when tests finish.

More details can be found in **Add test** section.

## Run the demo

On a linux machine, run the following command:

```bash
tests/integration/example/integration.sh
```

It builds mixs (mixer binary), downloads envoy binary and installs fortio binary from vendor directory
and brings these three components on local processes for two simple tests.

[Sample1](https://github.com/istio/istio/tree/master/tests/integration/example/tests/sample1)
shows how to reuse a test cases in different test environments

[Sample2](https://github.com/istio/istio/tree/master/tests/integration/example/tests/sample2)
shows how to reuse a test environment in different test cases


### Run a single test manually

```bash
go test -v ./tests/integration/example/tests/sample1 \
-envoy_binary <envoy_binary_path> \
-envoy_start_script <envoy_start_script_path> \
-mixer_binary <mixer_binary_path> \
-fortio_binary <fortio_binary_path>
```

One example:
```bash
go test -v ./tests/integration/example/tests/sample1
-envoy_binary /home/bootstrap/go/out/linux_amd64/release/envoy \
-envoy_start_script /home/bootstrap/go/src/istio.io/proxy/src/envoy/http/mixer/start_envoy \
-mixer_binary /home/bootstrap/go/out/linux_amd64/release/mixs \
-fortio_binary fortio
```


## File Structure
Under istio/tests/integration directory, it has three top level folders: **component, framework and example**.

* **component** is a centralized locations for existing components. New reusable components should be put here.
* **framework** has structures and interfaces for this integration test framework.
* **example** is a folder with demo showing how to use this framework. Developers can add their own tests based on the structure in the folder.

    * integration.sh is the entry script to build and trigger tests.
    * tests contains test files.
    * environment contains TestEnv implementations for tests here.


## Add tests

Besides the simple demos, there is another example of using this framework: [security integration](https://github.com/istio/istio/tree/master/security/tests/integration)

### Find the components or create new ones

Based on what modules you need (mixer, proxy and/or some applications) to test
and where you want to set up those modules (local process, local kube or actual kubernetes cluster),
one or more componensare required.

Different components will simply implement the Component interface. The Component interface essentially abstract out how a particular component / server can be started, stopped, and monitored.
Each component has a customized `configuration` and `status` for data sharing and communication between inside and outside of the component.
`Component` interface has GET and SET function for `configuration` and only GET for `status` since it's expected to be modified only inside this component.

We have a centralized location [istio/tests/integration/components](https://github.com/istio/istio/tree/master/tests/integration/component)
to keep components for reuse in different cases.
It is advised to reuse existing components if possible.
Also, you can update existing components to satisfy your requirement as long as the change is backward compatible and
does not break existing tests.  If you cannot find a component to reuse, you can create your own and put it in the same components directory
if it is can be reused by other developers and test cases.

* A component must implement Component interface with all methods:

    * `GetName()`: GetName return component name. This enforce every component to have a name.
    * `Start()`: Start defines how to step up and start this component. Will be called in testEnvManager.SetUp()
    * `Stop()`: Stop defines how to stop this component. Will be called in testEnvManager.TearDown()
    * `IsAlive()`: This is a method to check if this component is alive and working properly. It’s way to monitor if it’s being successfully started/stopped.
    * `GetConfig()`: GetConfig returns the config of this component for outside use. It can be called anytime during the whole component lifetime. It's recommended to use sync. Mutex to lock data while read in case component itself is accessing config.
    * `SetConfig(config Config)`: SetConfig sets a config into this component. Initial config is set when during initialization. But config can be updated at runtime using SetConfig. Component needs to implement extra functions to apply new configs. It's recommended to use sync.Mutex to lock data while write in case component itself is accessing config.
    * `GetStatus()`: GetStatus returns the status of this component for outside use. It can be called anytime during the whole component lifetime. It's recommended to use sync.Mutex to lock data while read in case component itself is updating status.

### Create a test environment

Create a test environment and define what components are included.

* A environment must implement environment interface with all methods:

    * `GetComponents()`: The key part, this method bakes components in this environment and returns them in a list. It should be singleton-like function
    * `GetName()`: GetName return environment name. This enforce every environment to have a name.
    * `Bringup()`: Preparations should be done for the whole environment
    * `Cleanup()`: Clean everything created by this test environment, not component level

### Create your test files
Create tests under your code directory and import framework package. 
Here are [two example test files](https://github.com/istio/istio/tree/master/tests/integration/example/tests). Create multiple test cases with the name “Testxxx” and then add a TestMain(). 
Only several things need to be included in `TestMain()`.

`testEM.RunTest(m)` handles bringing up environment, triggering tests and teardown.

```bash
var (
    testEM *framework.TestEnvManager
)

func TestMain(m *testing.M) {
      //Parse flags for environments and components
      flag.Parse()

      // Create an environment
      testEnv := mixerEnvoyEnv.NewMixerEnvoyEnv(“mixer_envoy_env”)
      
      // Feed the env to a testEnvManager testEM
      testEM = framework.NewTestEnvManager(testEnv, “sample_Test”)

      // Kick off tests through testEnvManager
      res := testEM.RunTest(m)

      log.Printf("Test result %d in env %s", res, mixerEnvoyEnvName)
      os.Exit(res)
}

```

