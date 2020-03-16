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
		KubeConfig: requireKubeConfigs(env.ISTIO_TEST_KUBE_CONFIG.Value()),
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

	var err error
	s.KubeConfig, err = parseKubeConfigs(kubeConfigs)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func requireKubeConfigs(value string) []string {
	out, err := parseKubeConfigs(value)
	if err != nil {
		panic(err)
	}
	return out
}

func parseKubeConfigs(value string) ([]string, error) {
	if len(value) == 0 {
		return []string{defaultKubeConfig()}, nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, f := range parts {
		if f != "" {
			if err := normalizeFile(&f); err != nil {
				return nil, err
			}
			out = append(out, f)
		}
	}
	return out, nil
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

func defaultKubeConfig() string {
	v := os.Getenv("KUBECONFIG")
	if len(v) > 0 {
		return v
	}
	return "~/.kube/config"
}

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&kubeConfigs, "istio.test.kube.config", strings.Join(settingsFromCommandLine.KubeConfig, ":"),
		"A comma-separated list of paths to kube config files for cluster environments (default is current kube context)")
	flag.BoolVar(&settingsFromCommandLine.Minikube, "istio.test.kube.minikube", settingsFromCommandLine.Minikube,
		"Indicates that the target environment is Minikube. Used by Ingress component to obtain the right IP address..")
}
