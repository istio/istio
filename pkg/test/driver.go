//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package test

import (
	"flag"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"istio.io/istio/pkg/test/impl/logging"
	"istio.io/istio/pkg/test/impl/tmp"
)

const (
	maxTestIDLength = 30
)

// Internal singleton for storing test environment.
type driverState struct {
	sync.Mutex

	testID string
	runID  string

	tmpDir string

	labels string

	initializedDependencies map[Dependency]interface{}
}

var driver = driverState{
	initializedDependencies: make(map[Dependency]interface{}),
}

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&driver.labels, "labels", "", "Filter tests based on the given label")
}

func setup(testID string) (err error) {
	driver.Lock()
	defer driver.Unlock()

	driver.testID = testID
	driver.runID = generateRunID(testID)
	if driver.tmpDir, err = tmp.Create(driver.runID); err != nil {
		return
	}

	if err = logging.Initialize(driver.runID); err != nil {
		return
	}

	return
}

func doCleanup() {
	driver.Lock()
	defer driver.Unlock()

	for k, v := range driver.initializedDependencies {
		k.Cleanup(v)
	}
}

func generateRunID(testID string) string {
	u := uuid.New().String()
	u = strings.Replace(u, "-", "", -1)
	testID = strings.Replace(testID, "_", "-", -1)
	// We want at least 6 characters of uuid padding
	padding := maxTestIDLength - len(testID)
	return fmt.Sprintf("%s-%s", testID, u[0:padding])
}
