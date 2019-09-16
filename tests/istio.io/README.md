# Istio.io Example Tests

## Overview
This folder contains tests for the different examples on Istio.io.

## Writing Tests
Tests in the Istio.io framework support the following test types: 
* Executing scripts using the `RunScript(script string, output outputType, verifier VerificationFunction)` method
This method accepts an argument which is the name of a script to run, a desired outputType (`TextOutput`,`YamlOutput`, and `JsonOutput`), and an optional verification function. If verifier is set to nil, then any errors from the script will be interpreted as a failure of that step.  
* Applying/Deleting files using `Apply(namespace string, path string)`/`Delete(namespace string, path string)`
These methods apply and delete the YAML file at path from the specified namespace.
* Executing custom Go tests by calling `Exec(testFunction testFunc)` where the argument is a function of the form func(t *testing.T) error. 
* Waiting for pods to deploy using the `W`aitForPods(fetchFunc KubePodFetchFunc)` function where KubePodFetchFunc follows the form func(env *kube.Environment) kubePkg.PodFetchFunc.
This method allows a test to wait for a set of pods (specified by the fetch function) to start, or for 30 seconds, whichever comes first.

To write a test, add the following imports to your file: 
```golang
    "istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/istio.io/examples"
```

Next, create a function called TestMain as follows. This function will setup the Istio environment used by the tests. The `istio.Setup` function accepts an optional method to customize the Istio environment deployed. 

```golang
func TestMain(m *testing.M) {
	framework.NewSuite("mtls-migration", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		RequireEnvironment(environment.Kube).
		Run()
}
```

After creating `TestMain`, tests can be created as follows. In this example, the NewMultiPodFetch function is waiting for all pods in the foo, bar,and legacy namespaces to start. The GetCurlVerifier method is verify the responses from the curl script. Finally, the run method triggers the test to run. 

```golang
func TestMTLS(t *testing.T) {
	//Test
	examples.New(t, "mtls-migration").
		RunScript("create-ns-foo-bar-legacy.sh", examples.TextOutput, nil).
		WaitForPods(examples.NewMultiPodFetch("foo")).
		RunScript("curl-foo-bar-legacy.sh", examples.TextOutput, examples.GetCurlVerifier([]string{"200", "200", "200"})).
		Run()
}
```

### Built in Verifiers
The framework includes some predefined verifiers for scripts. These are described below. 

#### examples.GetCurlVerifier
The GetCurlVerifier method is used to create a verifier that parses the output of a curl script and compares each response. If a response of `000` is returned from curl, the verifier will compare the next line against the provided output. Finally, the run method triggers the test to run. 

### Output
When the test framework runs, it creates a directory called output. This directory contains a copy of all YAML files, scripts, and a file for each script containing the output. 



## Executing Tests Locally using Minikube
* Create a Minikube environment as advised on istio.io
* Run the tests as follows:
 go test ./... -p 1 --istio.test.env kube --istio.test.kube.config ~/.kube/config -v

