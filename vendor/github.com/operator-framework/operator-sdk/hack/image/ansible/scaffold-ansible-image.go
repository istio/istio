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

package main

import (
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/ansible"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	log "github.com/sirupsen/logrus"
)

// main renders scaffolds that are required to build the ansible operator base
// image. It is intended for release engineering use only. After running this,
// you can place a binary in `build/_output/bin/ansible-operator` and then run
// `operator-sdk build`.
func main() {
	cfg := &input.Config{
		AbsProjectPath: projutil.MustGetwd(),
		ProjectName:    "ansible-operator",
	}

	s := &scaffold.Scaffold{}
	err := s.Execute(cfg,
		&ansible.DockerfileHybrid{},
		&ansible.Entrypoint{},
		&ansible.UserSetup{},
		&ansible.K8sStatus{},
		&ansible.AoLogs{},
	)
	if err != nil {
		log.Fatalf("Add scaffold failed: (%v)", err)
	}
}
