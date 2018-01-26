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
	`global:
  namespace: {{ .Namespace }}
  sidecar-injector:
    enabled: {{ .SidecarInjector }}
  proxy:
    hub: {{ .Hub }}
    tag: {{ .ProxyTag }}
    debug: {{ .Debug }}
  pilot:
    enabled: {{ .Pilot }}
    hub: {{ .Hub }}
    tag: {{ .PilotTag }}
  security:
    enabled: {{ .CA }}
    hub: {{ .Hub }}
    tag: {{ .CaTag }}
  mixer:
    enabled: {{ .Mixer }}
    hub: {{ .Hub }}
    tag: {{ .MixerTag }}
  ingress:
    use_nodeport: {{ gt .NodePort 0 }}
    nodeport_port: {{ .NodePort }}
  hyperkube_hub: {{ .HyperkubeHub }}
  hyperkube_tag: {{ .HyperkubeTag }}
`))

// fromModel returns a string representation of the values.yaml file for helm config
func valuesFromInstallation(i *installation) string {
	buf := &bytes.Buffer{}
	_ = valuesTemplate.Execute(buf, i)
	return buf.String()
}
