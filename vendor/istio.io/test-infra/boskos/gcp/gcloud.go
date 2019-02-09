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

package gcp

import (
	"os"

	"istio.io/test-infra/toolbox/util"
)

// SetKubeConfig saves kube config from a given cluster to the given location
func SetKubeConfig(project, zone, cluster, kubeconfig string) error {
	if err := os.Setenv("KUBECONFIG", kubeconfig); err != nil {
		return err
	}
	_, err := util.ShellSilent(
		"gcloud container clusters get-credentials %s --project=%s --zone=%s",
		cluster, project, zone)
	return err
}

// ActivateServiceAccount activates a service account for gcloud
func ActivateServiceAccount(serviceAccount string) error {
	_, err := util.ShellSilent(
		"gcloud auth activate-service-account --key-file=%s",
		serviceAccount)
	return err
}
