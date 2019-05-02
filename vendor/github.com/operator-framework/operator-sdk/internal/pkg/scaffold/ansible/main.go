// Copyright 2019 The Operator-SDK Authors
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

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

// Main - main source file for ansible operator
type Main struct {
	input.Input
}

func (m *Main) GetInput() (input.Input, error) {
	if m.Path == "" {
		m.Path = filepath.Join("cmd", "manager", "main.go")
	}
	m.TemplateBody = mainTmpl
	return m.Input, nil
}

const mainTmpl = `package main

import (
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	aoflags "github.com/operator-framework/operator-sdk/pkg/ansible/flags"
	"github.com/operator-framework/operator-sdk/pkg/ansible"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"

	"github.com/spf13/pflag"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

func main() {
	aflags := aoflags.AddTo(pflag.CommandLine)
	pflag.Parse()
	logf.SetLogger(zap.Logger())

	if err := ansible.Run(aflags); err != nil {
		logf.Log.WithName("cmd").Error(err, "")
		os.Exit(1)
	}
}
`
