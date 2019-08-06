package examples

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
)

type testStep interface {
	Run(t *testing.T)
	Copy(path string) error
}

func newStepFile(path string) testStep {
	return fileTestType{
		path: path,
	}
}

type fileTestType struct {
	path string
}

func (test fileTestType) Run(t *testing.T) {
	t.Logf(fmt.Sprintf("Executing %s\n", test.path))
	//todo: add file support
}

func (test fileTestType) Copy(path string) error {
	//todo: implement copy
	return nil
}

func newStepFunction(testFunction testFunc) testStep {
	return functionTestType{
		testFunction: testFunction,
	}
}

type functionTestType struct {
	testFunction testFunc
}

func (test functionTestType) Run(t *testing.T) {
	t.Logf(fmt.Sprintf("Executing function\n"))
	test.testFunction(t)
}

func (test functionTestType) Copy(path string) error {
	return nil
}

func newStepScript(script string, output outputType) testStep {
	return scriptTestType{
		script: script,
		output: output,
	}

	//todo: what to do for different output types?
}

type scriptTestType struct {
	script string
	output outputType
}

func (test scriptTestType) Run(t *testing.T) {
	t.Logf(fmt.Sprintf("Executing %s\n", test.script))
	cmd := exec.Command(test.script)

	output, err := cmd.CombinedOutput()
	/*if err != nil {
		t.Fatalf("test framework could not get script output: %s", err)
	}*/
	t.Logf("script output: %s err: %s\n", output, err)
}
func (test scriptTestType) Copy(path string) error {
	//todo: implement copy
	return nil
}

func copyFile(src string, dest string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}
	return nil
}
