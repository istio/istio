// Copyright 2017 Istio Authors
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

package file_test

import (
	"context"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"istio.io/istio/pilot/adapter/config/file"
	"istio.io/istio/pilot/adapter/config/memory"
	"istio.io/istio/pilot/model"
)

var (
	files = []string{
		"cb-policy.yaml",
		"timeout-route-rule.yaml",
		"weighted-route.yaml",
		"fault-route.yaml",
		"redirect-route.yaml",
		"rewrite-route.yaml",
		"websocket-route.yaml",
		"egress-rule.yaml",
		"egress-rule-cb-policy.yaml",
		"egress-rule-timeout-route-rule.yaml",
		"ingress-route-world.yaml",
		"ingress-route-foo.yaml",
	}
)

func createTempDir() string {
	// Make the temporary directory
	dir, _ := ioutil.TempDir("/tmp/", "monitor")
	_ = os.MkdirAll(dir, os.ModeDir|os.ModePerm)
	return dir
}

type event struct {
	config model.Config
	event  model.Event
}

type controllerManager struct {
	controller     model.ConfigStoreCache
	controllerStop chan struct{}
}

func (cm *controllerManager) setup(eventCh chan event) {
	cm.controllerStop = make(chan struct{})
	store := memory.Make(model.IstioConfigTypes)
	cm.controller = memory.NewControllerWithBufferSize(store, 100)

	// Register changes to the repository
	for _, s := range model.IstioConfigTypes.Types() {
		cm.controller.RegisterEventHandler(s, func(config model.Config, ev model.Event) {
			eventCh <- event{
				config: config,
				event:  ev,
			}
		})
	}

	// Run the controller.
	go cm.controller.Run(cm.controllerStop)
}

func (cm *controllerManager) teardown() {
	close(cm.controllerStop)
}

type env struct {
	controllerManager controllerManager
	monitorCtx        context.Context
	monitorCancel     context.CancelFunc
	eventCh           chan event
	fsroot            string
	monitor           *file.Monitor
}

func (e *env) setup(tempDir string) {
	e.eventCh = make(chan event)

	// Create and setup the controller.
	e.controllerManager = controllerManager{}
	e.controllerManager.setup(e.eventCh)

	e.monitorCtx, e.monitorCancel = context.WithCancel(context.Background())
	e.fsroot = tempDir

	// Make all of the updates go through the controller so that the events are triggered.
	e.monitor = file.NewMonitor(e.controllerManager.controller, e.fsroot, nil)
	e.monitor.Start(e.monitorCtx)
}

func (e *env) teardown() {
	e.monitorCancel()
	e.controllerManager.teardown()

	// Remove the temp dir.
	os.RemoveAll(e.fsroot)
}

func (e *env) watch() event {
	return <-e.eventCh
}

func (e *env) watchFor(eventType model.Event) event {
	for {
		if ev := e.watch(); ev.event == eventType {
			return ev
		}
	}
}

func copyTestFile(fileName string, toDir string) error {
	return copyFile("testdata/"+fileName, toDir+"/"+fileName)
}

func copyFile(src, dst string) error {
	data, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(dst, data, os.ModePerm)
}

func parseConfig(path string) (*model.Config, error) {
	configs, err := file.ParseYamlFile(path)
	if err != nil {
		return nil, err
	}
	return configs[0], nil
}

func TestAdd(t *testing.T) {
	e := env{}
	e.setup(createTempDir())
	defer e.teardown()

	for _, fileName := range files {
		t.Run(fileName, func(t *testing.T) {
			// Get the configuration that we expect to be added.
			expectedCfg, err := parseConfig("testdata/" + fileName)
			if err != nil {
				t.Fatalf("Unable to parse config: %s", fileName)
				return
			}

			// Add the configuration.
			if err := copyTestFile(fileName, e.fsroot); err != nil {
				t.Fatalf("Failed adding config file: %s", fileName)
				return
			}

			// Wait for the add to occur.
			event := e.watch()

			// Verify that the configuration was added properly.
			if event.event != model.EventAdd {
				t.Fatalf("Expected add event: %s", event)
				return
			}

			// Need to clear the resource version before comparing.
			if !reflect.DeepEqual(*expectedCfg, event.config) {
				t.Fatalf("Config does not match.\nExpected:\n%s\nReceived:\n%s", *expectedCfg, event.config)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	// Copy all of the files over to the temp dir before initializing the environment.
	tempDir := createTempDir()
	for _, fileName := range files {
		if err := copyTestFile(fileName, tempDir); err != nil {
			t.Fatalf("Failed adding config file: %s", fileName)
			return
		}
	}

	e := env{}
	e.setup(tempDir)
	defer e.teardown()

	for _, fileName := range files {
		t.Run(fileName, func(t *testing.T) {
			// Get the configuration that we expect to be deleted
			expectedCfg, err := parseConfig("testdata/" + fileName)
			if err != nil {
				t.Fatalf("Unable to parse config: %s", fileName)
				return
			}

			err = os.Remove(e.fsroot + "/" + fileName)
			if err != nil {
				t.Fatalf("Unable to remove file: %s", fileName)
			}

			event := e.watchFor(model.EventDelete)

			// Need to clear the resource version before comparing.
			event.config.ResourceVersion = ""
			if !reflect.DeepEqual(*expectedCfg, event.config) {
				t.Fatalf("Config does not match.\nExpected:\n%s\nReceived:\n%s", *expectedCfg, event.config)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	// Copy all of the files over to the temp dir before initializing the environment.
	tempDir := createTempDir()
	for _, fileName := range files {
		if err := copyTestFile(fileName, tempDir); err != nil {
			t.Fatalf("Failed adding config file: %s", fileName)
			return
		}
	}

	e := env{}
	e.setup(tempDir)
	defer e.teardown()

	// Overwrite the original file with the updated.
	updateFilePath := "testdata/cb-policy.yaml.update"
	copyFile(updateFilePath, e.fsroot+"/"+"cb-policy.yaml")

	// Get the configuration that we expect to be deleted
	expectedCfg, err := parseConfig(updateFilePath)
	if err != nil {
		t.Fatalf("Unable to parse config: %s", updateFilePath)
		return
	}

	event := e.watchFor(model.EventUpdate)

	// Need to clear the resource version before comparing.
	event.config.ResourceVersion = ""
	if !reflect.DeepEqual(*expectedCfg, event.config) {
		t.Fatalf("Config does not match.\nExpected:\n%s\nReceived:\n%s", *expectedCfg, event.config)
	}
}
