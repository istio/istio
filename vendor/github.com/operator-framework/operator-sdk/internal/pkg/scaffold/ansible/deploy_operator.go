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

package ansible

import (
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const DeployOperatorFile = "operator.yaml"

type DeployOperator struct {
	input.Input
	IsClusterScoped bool
}

// GetInput - gets the input
func (d *DeployOperator) GetInput() (input.Input, error) {
	if d.Path == "" {
		d.Path = filepath.Join(scaffold.DeployDir, DeployOperatorFile)
	}
	d.TemplateBody = deployOperatorAnsibleTmpl

	return d.Input, nil
}

const deployOperatorAnsibleTmpl = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.ProjectName}}
spec:
  replicas: 1
  selector:
    matchLabels:
      name: {{.ProjectName}}
  template:
    metadata:
      labels:
        name: {{.ProjectName}}
    spec:
      serviceAccountName: {{.ProjectName}}
      containers:
        - name: ansible
          command:
          - /usr/local/bin/ao-logs
          - /tmp/ansible-operator/runner
          - stdout
          # Replace this with the built image name
          image: "{{ "{{ REPLACE_IMAGE }}" }}"
          imagePullPolicy: "{{ "{{ pull_policy|default('Always') }}"}}"
          volumeMounts:
          - mountPath: /tmp/ansible-operator/runner
            name: runner
            readOnly: true
        - name: operator
          # Replace this with the built image name
          image: "{{ "{{ REPLACE_IMAGE }}" }}"
          imagePullPolicy: "{{ "{{ pull_policy|default('Always') }}"}}"
          volumeMounts:
          - mountPath: /tmp/ansible-operator/runner
            name: runner
          env:
            - name: WATCH_NAMESPACE
              {{- if .IsClusterScoped }}
              value: ""
              {{- else }}
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
              {{- end}}
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: OPERATOR_NAME
              value: "{{.ProjectName}}"
      volumes:
        - name: runner
          emptyDir: {}
`
