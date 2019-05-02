/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tiller

import (
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	reltesting "k8s.io/helm/pkg/releasetesting"
)

const maxParallelism = 20

// RunReleaseTest runs pre-defined tests stored as hooks on a given release
func (s *ReleaseServer) RunReleaseTest(req *services.TestReleaseRequest, stream services.ReleaseService_RunReleaseTestServer) error {

	if err := validateReleaseName(req.Name); err != nil {
		s.Log("releaseTest: Release name is invalid: %s", req.Name)
		return err
	}

	// finds the non-deleted release with the given name
	rel, err := s.env.Releases.Last(req.Name)
	if err != nil {
		return err
	}

	testEnv := &reltesting.Environment{
		Namespace:   rel.Namespace,
		KubeClient:  s.env.KubeClient,
		Timeout:     req.Timeout,
		Stream:      stream,
		Parallel:    req.Parallel,
		Parallelism: maxParallelism,
	}
	s.Log("running tests for release %s", rel.Name)
	tSuite, err := reltesting.NewTestSuite(rel)
	if err != nil {
		s.Log("error creating test suite for %s: %s", rel.Name, err)
		return err
	}

	if err := tSuite.Run(testEnv); err != nil {
		s.Log("error running test suite for %s: %s", rel.Name, err)
		return err
	}

	rel.Info.Status.LastTestSuiteRun = &release.TestSuite{
		StartedAt:   tSuite.StartedAt,
		CompletedAt: tSuite.CompletedAt,
		Results:     tSuite.Results,
	}

	if req.Cleanup {
		testEnv.DeleteTestPods(tSuite.TestManifests)
	}

	if err := s.env.Releases.Update(rel); err != nil {
		s.Log("test: Failed to store updated release: %s", err)
	}

	return nil
}
