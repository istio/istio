//  Copyright 2018 Istio Authors
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

	"istio.io/istio/galley/pkg/runtime/resource"
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
var fst *fsTestSourceState

type fsTestSourceState struct {
	ConfigFiles map[string][]byte
	rootPath    string
}

func (fst *fsTestSourceState) testSetup(t *testing.T) {
	var err error

	fst.rootPath, err = ioutil.TempDir("", "configPath")
	if err != nil {
		t.Fatal(err)
	}

	for name, content := range fst.ConfigFiles {
		err = ioutil.WriteFile(filepath.Join(fst.rootPath, name), content, 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func (fst *fsTestSourceState) writeFile() error {
	for name, content := range fst.ConfigFiles {
		err := ioutil.WriteFile(filepath.Join(fst.rootPath, name), content, 0600)
		if err != nil {
			return err
		}
	}
	return nil
}

func (fst *fsTestSourceState) deleteFile() error {
	for name := range fst.ConfigFiles {
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

func TestNewSource(t *testing.T) {
	fst = &fsTestSourceState{
		ConfigFiles: map[string][]byte{},
	}
	fst.testSetup(t)
	defer fst.testTeardown(t)
	s, err := New(fst.rootPath)
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}

	if s == nil {
		t.Fatal("expected non-nil source")
	}
}
func TestSource_InitialScan(t *testing.T) {
	fst = &fsTestSourceState{
		ConfigFiles: map[string][]byte{"virtual_service.yml": []byte(virtualServiceYAML)},
	}
	fst.testSetup(t)
	defer fst.testTeardown(t)
	s, err := New(fst.rootPath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	log := logChannelOutput(ch, 1)
	expected := strings.TrimSpace(`
	[Event](Added: [VKey](type.googleapis.com/istio.networking.v1alpha3.VirtualService:route-for-myapp @v0))`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestSource_AddEvents(t *testing.T) {
	fst = &fsTestSourceState{
		ConfigFiles: map[string][]byte{},
	}
	fst.testSetup(t)
	defer fst.testTeardown(t)
	s, err := New(fst.rootPath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	fst.ConfigFiles["virtual_service.yml"] = []byte(virtualServiceYAML)

	err = fst.writeFile()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	log := logChannelOutput(ch, 1)
	expected := strings.TrimSpace(`
	[Event](Added: [VKey](type.googleapis.com/istio.networking.v1alpha3.VirtualService:route-for-myapp @v1))`)

	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestSource_DeleteEvents(t *testing.T) {

	fst = &fsTestSourceState{
		ConfigFiles: map[string][]byte{"virtual_service.yml": []byte(virtualServiceYAML)},
	}
	fst.testSetup(t)
	defer fst.testTeardown(t)
	s, err := New(fst.rootPath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()

	err = fst.deleteFile()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	log := logChannelOutput(ch, 2)
	expected := strings.TrimSpace(`
	[Event](Deleted: [VKey](type.googleapis.com/istio.networking.v1alpha3.VirtualService:route-for-myapp @v0))`)

	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestFsSource_ModifyFile(t *testing.T) {
	fst = &fsTestSourceState{
		ConfigFiles: map[string][]byte{"virtual_service.yml": []byte(virtualServiceYAML)},
	}
	fst.testSetup(t)
	defer fst.testTeardown(t)
	s, err := New(fst.rootPath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	err = ioutil.WriteFile(filepath.Join(fst.rootPath, "virtual_service.yml"), []byte(virtualServiceChangedYAML), 0600)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	log := logChannelOutput(ch, 3)
	expected := strings.TrimSpace(`
	[Event](Added: [VKey](type.googleapis.com/istio.networking.v1alpha3.VirtualService:route-for-myapp-changed @v1))`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestFsSource_DeletePartResorceInFile(t *testing.T) {
	fst = &fsTestSourceState{
		ConfigFiles: map[string][]byte{"mixer.yml": []byte(mixerYAML)},
	}
	fst.testSetup(t)
	defer fst.testTeardown(t)
	s, err := New(fst.rootPath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	fst.ConfigFiles["mixer.yml"] = []byte(mixerPartYAML)

	err = fst.writeFile()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	log := logChannelOutput(ch, 4)
	expected := "[Event](Deleted: [VKey](type.googleapis.com/istio.policy.v1beta1.Rule:some.mixer.rule @v0))"

	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
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
