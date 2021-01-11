// Copyright Istio Authors
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

package boskos

import (
	"fmt"
	"os"

	"sigs.k8s.io/boskos/client"
	"sigs.k8s.io/kubetest2/pkg/boskos"
)

const (
	boskosLocation       = "http://boskos.test-pods.svc.cluster.local."
	boskosAcquireTimeout = 600
)

// AcquireBoskosResource returns a boskos resource name of the given type.
// Parameters: $1 - resource type. Must be one of the types configured in https://gke-internal.googlesource.com/istio/test-infra-internal/+/refs/heads/master/boskos/config/resources.yaml.
func AcquireBoskosResource(resourceType string) (string, error) {
	cli, err := client.NewClient(os.Getenv("JOB_NAME"), boskosLocation, "", "")
	if err != nil {
		return "", fmt.Errorf("error creating the boskos client: %w", err)
	}
	r, err := boskos.Acquire(cli, resourceType, boskosAcquireTimeout, make(chan struct{}))
	if err != nil {
		return "", fmt.Errorf("error acquiring a boskos resource with type %s: %w", resourceType, err)
	}
	return r.Name, nil
}

// ReleaseBoskosResource releases a leased boskos resource.
// Parameters: $1 - resource name. Must be the same as returned by the
// 					boskos_acquire function call, e.g. asm-boskos-1.
func ReleaseBoskosResource(resourceName string) error {
	cli, err := client.NewClient(os.Getenv("JOB_NAME"), boskosLocation, "", "")
	if err != nil {
		return fmt.Errorf("error creating the boskos client: %w", err)
	}
	if err := boskos.Release(cli, resourceName, make(chan struct{})); err != nil {
		return fmt.Errorf("error releasing boskos resource %s: %w", resourceName, err)
	}
	return nil
}
