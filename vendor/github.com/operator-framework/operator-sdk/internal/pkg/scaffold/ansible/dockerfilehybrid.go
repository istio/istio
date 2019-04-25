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

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

//DockerfileHybrid - Dockerfile for a hybrid operator
type DockerfileHybrid struct {
	input.Input

	// Playbook - if true, include a COPY statement for playbook.yml
	Playbook bool

	// Roles - if true, include a COPY statement for the roles directory
	Roles bool

	// Watches - if true, include a COPY statement for watches.yaml
	Watches bool
}

// GetInput - gets the input
func (d *DockerfileHybrid) GetInput() (input.Input, error) {
	if d.Path == "" {
		d.Path = filepath.Join(scaffold.BuildDir, scaffold.DockerfileFile)
	}
	d.TemplateBody = dockerFileHybridAnsibleTmpl
	return d.Input, nil
}

const dockerFileHybridAnsibleTmpl = `FROM ansible/ansible-runner

RUN yum remove -y ansible python-idna
RUN yum install -y inotify-tools && yum clean all
RUN pip uninstall ansible-runner -y

RUN pip install --upgrade setuptools
RUN pip install ansible ansible-runner openshift kubernetes ansible-runner-http idna==2.7

RUN mkdir -p /etc/ansible \
    && echo "localhost ansible_connection=local" > /etc/ansible/hosts \
    && echo '[defaults]' > /etc/ansible/ansible.cfg \
    && echo 'roles_path = /opt/ansible/roles' >> /etc/ansible/ansible.cfg \
    && echo 'library = /usr/share/ansible/openshift' >> /etc/ansible/ansible.cfg

ENV OPERATOR=/usr/local/bin/ansible-operator \
    USER_UID=1001 \
    USER_NAME=ansible-operator\
    HOME=/opt/ansible

{{- if .Watches }}
COPY watches.yaml ${HOME}/watches.yaml{{ end }}

# install operator binary
COPY build/_output/bin/{{.ProjectName}} ${OPERATOR}
# install k8s_status Ansible Module
COPY library/k8s_status.py /usr/share/ansible/openshift/

COPY bin /usr/local/bin
RUN  /usr/local/bin/user_setup

{{- if .Roles }}
COPY roles/ ${HOME}/roles/{{ end }}
{{- if .Playbook }}
COPY playbook.yml ${HOME}/playbook.yml{{ end }}

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
`
