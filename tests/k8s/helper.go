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

// Package k8s provides helpers for testing k8s
package k8s

import (
	"log"
	"os"
	"os/user"
)

// Kubeconfig returns the config to use for testing.
func Kubeconfig(relpath string) string {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig != "" {
		return kubeconfig
	}

	// For Bazel sandbox we search a different location:
	// Attempt to use the relpath, using the linked file - pilot/kube/platform/config
	kubeconfig, _ = os.Getwd()
	kubeconfig = kubeconfig + relpath
	if _, err := os.Stat(kubeconfig); err == nil {
		return kubeconfig
	}

	// Fallback to the user's default config
	log.Println("Using user home k8s config - might affect real cluster ! Not found: ", kubeconfig)
	usr, err := user.Current()
	if err == nil {
		kubeconfig = usr.HomeDir + "/.kube/config"
	}

	return kubeconfig
}
