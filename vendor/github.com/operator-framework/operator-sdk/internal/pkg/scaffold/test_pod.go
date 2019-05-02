// Copyright 2018 The Operator-SDK Authors
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

package scaffold

import (
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const TestPodYamlFile = "test-pod.yaml"

type TestPod struct {
	input.Input

	// Image is the image name used for testing, ex. quay.io/repo/operator-image
	Image string

	// TestNamespaceEnv is an env variable specifying the test namespace
	TestNamespaceEnv string
}

func (s *TestPod) GetInput() (input.Input, error) {
	if s.Path == "" {
		s.Path = filepath.Join(DeployDir, TestPodYamlFile)
	}
	s.TemplateBody = testPodTmpl
	return s.Input, nil
}

const testPodTmpl = `apiVersion: v1
kind: Pod
metadata:
  name: {{.ProjectName}}-test
spec:
  restartPolicy: Never
  containers:
  - name: {{.ProjectName}}-test
    image: {{.Image}}
    imagePullPolicy: Always
    command: ["/go-test.sh"]
    env:
      - name: {{.TestNamespaceEnv}}
        valueFrom:
          fieldRef:
            fieldPath: metadata.namespace
`
