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

package framework

import (
	"io/ioutil"
	"log"
	"os"
)

// CommonEnv is a base environment type for most environment
type CommonEnv struct {
	TestEnv
	EnvID  string
	TmpDir string
}

// GetName implement the function in TestEnv return environment ID
func (ce *CommonEnv) GetName() string {
	return ce.EnvID
}

// Bringup implement the function in TestEnv
// Create local temp dir. Can be manually overrided if necessary.
func (ce *CommonEnv) Bringup() (err error) {
	log.Printf("Bringing up %s", ce.EnvID)
	ce.TmpDir, err = ioutil.TempDir("", ce.GetName())
	return
}

// Cleanup implement the function in TestEnv
// Remove the local temp dir. Can be manually overrided if necessary.
func (ce *CommonEnv) Cleanup() (err error) {
	log.Printf("Cleaning up %s", ce.EnvID)
	os.RemoveAll(ce.TmpDir)
	return
}
