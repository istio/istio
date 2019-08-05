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

package kube

import (
	"flag"
	"os"
	"strings"

	"github.com/mitchellh/go-homedir"

	"istio.io/istio/pkg/test/env"
)

var (
	// Settings we will collect from the command-line.
	settingsFromCommandLine = &Settings{
		KubeConfig: strings.Split(env.ISTIO_TEST_KUBE_CONFIG.Value(), ":"),
	}
	// hold kubeconfigs from command line to split later
	kubeConfigs string
)

// newSettingsFromCommandline returns Settings obtained from command-line flags. flag.Parse must be called before calling this function.
func newSettingsFromCommandline() (*Settings, error) {
	if !flag.Parsed() {
		panic("flag.Parse must be called before this function")
	}

	s := settingsFromCommandLine.clone()

	s.KubeConfig = strings.Split(kubeConfigs, ":")

	if len(s.KubeConfig) != 0 {
		// iterate over list of paths, checking each
		for i := range s.KubeConfig {
			if s.KubeConfig[i] != "" {
				if err := normalizeFile(&s.KubeConfig[i]); err != nil {
					return nil, err
				}
			}
		}
	}

	return s, nil
}

func normalizeFile(path *string) error {
	// trim leading/trailing spaces from the path and if it uses the homedir ~, expand it.
	var err error
	*path = strings.TrimSpace(*path)
	(*path), err = homedir.Expand(*path)
	if err != nil {
		return err
	}

	return checkFileExists(*path)
}

func checkFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return err
	}
	return nil
}

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&kubeConfigs, "istio.test.kube.config", strings.Join(settingsFromCommandLine.KubeConfig, ":"),
		"A : seperated list of paths to kube config files for cluster environments (default is current kube context)")
	flag.BoolVar(&settingsFromCommandLine.Minikube, "istio.test.kube.minikube", settingsFromCommandLine.Minikube,
		"Indicates that the target environment is Minikube. Used by Ingress component to obtain the right IP address..")
}
