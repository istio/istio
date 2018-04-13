// Copyright 2017,2018 Istio Authors
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
	"fmt"

	"istio.io/istio/tests/util"
)

// TestConfig holds the common config, a deployable config and the application directory
type TestConfig struct {
	*CommonConfig
	AppDir      string
	extraConfig *DeployableConfig
}

// NewTestConfig creates a new TestConfig
func NewTestConfig(cc *CommonConfig, appDir string, extraConfig *DeployableConfig) *TestConfig {
	return &TestConfig{
		CommonConfig: cc,
		AppDir:       appDir,
		extraConfig:  extraConfig,
	}
}

// Setup initializes the test environment and waits for all pods to be in the running state.
func (t *TestConfig) Setup() error {
	// Deploy additional configuration.
	err := t.extraConfig.Setup()

	// Wait for all the pods to be in the running state before starting tests.
	if err == nil && !util.CheckPodsRunning(t.Kube.Namespace, t.Kube.KubeConfig) {
		err = fmt.Errorf("can't get all pods running")
	}

	return err
}

// Teardown shuts down the test environment.
func (t *TestConfig) Teardown() error {
	// Remove additional configuration.
	return t.extraConfig.Teardown()
}
