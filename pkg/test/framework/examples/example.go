package examples

import (
	"fmt"
	"os"
	"testing"
)

type outputType string

const (
	//TextOutput describes a test which returns text output
	TextOutput outputType = "text"
	//YamlOutput describes a test which returns yaml output
	YamlOutput outputType = "yaml"
	//JSONOutput describes a test which returns json output
	JSONOutput outputType = "json"
)

const (
	istioPath = "istio.io/istio/"
)

//CreateExample returns an instance of an example test
func CreateExample(t *testing.T, name string) Example {
	return Example{
		name:  name,
		steps: make([]testStep, 0),
		t:     t,
	}
}

//Example manages the steps in a test, executes them, and records the output
type Example struct {
	name  string
	steps []testStep
	t     *testing.T
}

//AddScript adds a directive to run a script
func (example *Example) AddScript(script string, output outputType) {
	//fullPath := getFullPath(istioPath + script)
	example.steps = append(example.steps, newStepScript("./"+script, output))
}

//AddFile adds an existing file
func (example *Example) AddFile(path string) {
	fullPath := getFullPath(istioPath + path)
	example.steps = append(example.steps, newStepFile(fullPath))
	panic("Not yet implemneted")
}

//todo: get last script run output
type testFunc func(t *testing.T)

//Exec registers a callback to be invoked synchronously. This is typically used for
//validation logic to ensure command-lines worked as intended
func (example *Example) Exec(testFunction testFunc) {
	example.steps = append(example.steps, newStepFunction(testFunction))
}

// getFullPath reads the current gopath from environment variables and appends
// the specified path to it
func getFullPath(path string) string {
	gopath := os.Getenv("GOPATH")
	return gopath + "/src/" + path

}

//Run runs the scripts and capture output
//Note that this overrides os.Stdout/os.Stderr and is not thread-safe
func (example *Example) Run() {

	//override stdout and stderr for test. Is there a better way of doing this?
	/*prevStdOut := os.Stdout
	prevStdErr := os.Stderr
	defer func() {
		os.Stdout = prevStdOut
		os.Stderr = prevStdErr
	}()*/

	//f, err := os.Create(
	//os.StdOut =

	example.t.Log(fmt.Sprintf("Executing test %s (%d steps)", example.name, len(example.steps)))
	//create directory if it doesn't exist
	if _, err := os.Stat(example.name); os.IsNotExist(err) {
		err := os.Mkdir(example.name, os.ModePerm)
		if err != nil {
			example.t.Fatalf("test framework failed to create directory: %s", err)
		}
	}

	for _, step := range example.steps {
		step.Run(example.t)
	}
}
