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

package settings

import (
	"flag"
	"fmt"
	"os"

	"istio.io/istio/pkg/log"
)

var (
	settings = defaultSettings()
	// TODO(nmittler): Add logging options to flags.
	logOptions = log.DefaultOptions()
)

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&settings.WorkDir, "work_dir", os.TempDir(),
		"Local working directory for creating logs/temp files. If left empty, os.TempDir() is used.")

	flag.StringVar((*string)(&settings.Environment), "environment", string(settings.Environment),
		fmt.Sprintf("Specify the environment to run the tests against. Allowed values are: [%s, %s]",
			Local, Kubernetes))

	flag.StringVar(&settings.KubeConfig, "config", settings.KubeConfig,
		"The path to the kube config file for cluster environments")

	flag.BoolVar(&settings.NoCleanup, "no-cleanup", settings.NoCleanup, "Do not cleanup resources after test completion")
}

func processFlags() error {
	// First, apply the environment variables.
	applyEnvironmentVariables(settings)

	flag.Parse()

	if err := log.Configure(logOptions); err != nil {
		return err
	}

	// TODO: Instead of using hub/tag, we should be using the local registry to load images from.
	// See https://github.com/istio/istio/issues/6178 for details.

	// Capture environment variables
	hub := os.Getenv("HUB")
	tag := os.Getenv("TAG")
	settings.Hub = hub
	settings.Tag = tag

	return nil
}

func applyEnvironmentVariables(a *Settings) {
	if ISTIO_TEST_KUBE_CONFIG.Value() != "" {
		a.KubeConfig = ISTIO_TEST_KUBE_CONFIG.Value()
	}

	if ISTIO_TEST_ENVIRONMENT.Value() != "" {
		a.Environment = EnvironmentID(ISTIO_TEST_ENVIRONMENT.Value())
	}
}
