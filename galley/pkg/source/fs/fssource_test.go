// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/galley/pkg/meshconfig"
	kubeMeta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/converter"
	sn "istio.io/istio/pkg/mcp/snapshot"
)

var mixerYAML = `
apiVersion: "config.istio.io/v1alpha2"
kind: denier
metadata:
  name: some.mixer.denier
spec:
  status:
    code: 7
    message: Not allowed
---
apiVersion: "config.istio.io/v1alpha2"
kind: checknothing
metadata:
  name: some.mixer.checknothing
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: some.mixer.rule
spec:
  match: destination.labels["app"] == "someapp"
  actions:
  - handler: some.denier
    instances: [ some.checknothing ]
`

var mixerPartYAML = `
apiVersion: "config.istio.io/v1alpha2"
kind: denier
metadata:
  name: some.mixer.denier
spec:
  status:
    code: 7
    message: Not allowed
---
apiVersion: "config.istio.io/v1alpha2"
kind: checknothing
metadata:
  name: some.mixer.checknothing
spec:
`

var virtualServiceYAML = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-for-myapp
spec:
  hosts:
  - some.example.com
  gateways:
  - some-ingress
  http:
  - route:
    - destination:
        host: some.example.internal
`

var virtualServiceChangedYAML = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-for-myapp-changed
spec:
  hosts:
  - some.example.com
  gateways:
  - some-ingress
  http:
  - route:
    - destination:
        host: some.example.internal
`
var (
	fst *fsTestSourceState
)

type fsTestSourceState struct {
	// configFiles are file input for istio config
	configFiles map[string][]byte
	// rootPath is where the testing will write the files in
	rootPath string
}

type scenario struct {
	// initFile is the initial content for istio config
	initFile string
	// initFileName is the initial istio config file name
	initFileName string
	// expectedResult is the expected event for the testing
	expectedResult string
	// expectedSequence is the expected event sequence in the channel
	expectedSequence int
	// fileActin is the file operations for the testing
	fileAction func(chan resource.Event, runtime.Source)
	// checkResult is how the testing check result
	checkResult func(chan resource.Event, string, *testing.T, int)
}

var checkResult = func(ch chan resource.Event, expected string, t *testing.T, expectedSequence int) {
	t.Helper()

	log := logChannelOutput(ch, expectedSequence)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}
}

func (fst *fsTestSourceState) testSetup(t *testing.T) {
	t.Helper()

	var err error

	fst.rootPath, err = ioutil.TempDir("", "configPath")
	if err != nil {
		t.Fatal(err)
	}

	for name, content := range fst.configFiles {
		err = ioutil.WriteFile(filepath.Join(fst.rootPath, name), content, 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func (fst *fsTestSourceState) writeFile() error {
	for name, content := range fst.configFiles {
		err := ioutil.WriteFile(filepath.Join(fst.rootPath, name), content, 0600)
		if err != nil {
			return err
		}
	}
	return nil
}

func (fst *fsTestSourceState) deleteFile() error {
	for name := range fst.configFiles {
		err := os.Remove(filepath.Join(fst.rootPath, name))
		if err != nil {
			return err
		}
	}
	return nil
}

func (fst *fsTestSourceState) testTeardown(t *testing.T) {
	err := os.RemoveAll(fst.rootPath)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFsSource(t *testing.T) {
	scenarios := map[string]scenario{
		"NewSource": {
			initFile:     "",
			initFileName: "",
			fileAction:   func(_ chan resource.Event, _ runtime.Source) {},
			checkResult:  nil,
		},
		"FsSource_InitialScan": {
			initFile:         virtualServiceYAML,
			initFileName:     "virtual_service.yml",
			expectedSequence: 2,
			expectedResult: strings.TrimSpace(`
			[Event](Added: [VKey](istio/networking/v1alpha3/virtualservices:route-for-myapp @v0))`),
			fileAction:  func(_ chan resource.Event, _ runtime.Source) {},
			checkResult: checkResult},
		"FsSource_AddFile": {
			initFile:         "",
			initFileName:     "",
			expectedSequence: 2,
			expectedResult: strings.TrimSpace(`
			[Event](Added: [VKey](istio/networking/v1alpha3/virtualservices:route-for-myapp @v1))`),
			fileAction: func(_ chan resource.Event, _ runtime.Source) {
				fst.configFiles["virtual_service.yml"] = []byte(virtualServiceYAML)
				err := fst.writeFile()
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			},
			checkResult: checkResult,
		},
		"FsSource_DeleteFile": {
			initFile:         virtualServiceYAML,
			initFileName:     "virtual_service.yml",
			expectedSequence: 3,
			expectedResult: strings.TrimSpace(`
			[Event](Deleted: [VKey](istio/networking/v1alpha3/virtualservices:route-for-myapp @v0))`),
			fileAction: func(_ chan resource.Event, _ runtime.Source) {
				err := fst.deleteFile()
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			},
			checkResult: checkResult,
		},
		"FsSource_ModifyFile": {
			initFile:         virtualServiceYAML,
			initFileName:     "virtual_service.yml",
			expectedSequence: 4,
			expectedResult: strings.TrimSpace(`
			[Event](Added: [VKey](istio/networking/v1alpha3/virtualservices:route-for-myapp-changed @v1))`),
			fileAction: func(_ chan resource.Event, _ runtime.Source) {
				err := ioutil.WriteFile(filepath.Join(fst.rootPath, "virtual_service.yml"), []byte(virtualServiceChangedYAML), 0600)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			},
			checkResult: checkResult,
		},
		"FsSource_DeletePartResorceInFile": {
			initFile:     mixerYAML,
			initFileName: "mixer.yml",
			fileAction: func(ch chan resource.Event, _ runtime.Source) {
				fst.configFiles["mixer.yml"] = []byte(mixerPartYAML)
				err := fst.writeFile()
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				donec := make(chan bool)
				expected := "[Event](Deleted: [VKey](istio/policy/v1beta1/rules:some.mixer.rule @v0))"
				go checkEventOccurs(expected, ch, donec)
				select {
				case <-time.After(5 * time.Second):
					t.Fatalf("Expected Event does not occur:\n%s\n", expected)
				case <-donec:
					return
				}
			},
			checkResult: nil,
		},
		"FsSource_publishEvent": {
			initFile:     virtualServiceYAML,
			initFileName: "virtual_service.yml",
			fileAction: func(_ chan resource.Event, s runtime.Source) {
				d := runtime.NewInMemoryDistributor()
				cfg := &runtime.Config{Mesh: meshconfig.NewInMemory()}
				processor := runtime.NewProcessor(s, d, cfg)
				err := processor.Start()
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				ch := make(chan bool)
				listenerAction := func(sp sn.Snapshot) {
					ch <- true
				}
				cancel := make(chan bool)
				defer func() { close(cancel) }()
				go d.ListenChanges(cancel, listenerAction)
				select {
				case <-ch:
					return
				case <-time.After(5 * time.Second):
					t.Fatal("The snapshot should have been set")
				}
				processor.Stop()
			},
			checkResult: nil,
		},
	}
	for name, scenario := range scenarios {
		t.Run(name, func(tt *testing.T) {
			runTestCode(tt, scenario)
		})
	}

}
func runTestCode(t *testing.T, test scenario) {
	if test.initFile == "" {
		fst = &fsTestSourceState{
			configFiles: map[string][]byte{},
		}
	} else {
		fst = &fsTestSourceState{
			configFiles: map[string][]byte{test.initFileName: []byte(test.initFile)},
		}
	}
	fst.testSetup(t)
	defer fst.testTeardown(t)
	s, err := New(fst.rootPath, kubeMeta.Types, &converter.Config{Mesh: meshconfig.NewInMemory()})
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}

	if s == nil {
		t.Fatal("expected non-nil source")
	}
	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	test.fileAction(ch, s)
	if test.checkResult != nil {
		test.checkResult(ch, test.expectedResult, t, test.expectedSequence)
	}
	s.Stop()
}

//Only log the last event out
func logChannelOutput(ch chan resource.Event, count int) string {
	var result resource.Event
	for i := 0; i < count; i++ {
		result = <-ch
	}
	return strings.TrimSpace(fmt.Sprintf("%v\n", result))
}

// Check whether a specif event occurs
func checkEventOccurs(expectedEvent string, ch chan resource.Event, donec chan bool) {
	for event := range ch {
		if expectedEvent == strings.TrimSpace(fmt.Sprintf("%v\n", event)) {
			donec <- true
		}
	}
}
