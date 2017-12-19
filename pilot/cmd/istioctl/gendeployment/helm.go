// Copyright 2017 Istio Authors.
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

package gendeployment

import (
	"bytes"
	"text/template"
)

var valuesTemplate = template.Must(template.New("helm").Parse(
	`Istio:
   deploy_base_config: true
   initializer_enabled: {{ .Initializer }}
   mixer_enabled: {{ .Mixer }}
   pilot_enabled: {{ .Pilot }}
   ingress:
      use_nodeport: {{ gt .NodePort 0 }}
      nodeport_port: {{ .NodePort }}

global:
   auth_enabled: {{ .CA }}
   namespace: {{ .Namespace }}
   ca_hub: {{ .Hub }}
   ca_tag: {{ .CaTag }}
   ca_demo: false
   proxy_hub: {{ .Hub }}
   proxy_tag: {{ .ProxyTag }}
   proxy_debug: {{ .Debug }}
   pilot_hub: {{ .Hub }}
   pilot_tag: {{ .PilotTag }}
`))

// fromModel returns a string representation of the values.yaml file for helm config
func valuesFromInstallation(i *installation) string {
	buf := &bytes.Buffer{}
	_ = valuesTemplate.Execute(buf, i)
	return buf.String()
}
