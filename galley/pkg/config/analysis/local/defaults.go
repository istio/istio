// Copyright 2019 Istio Authors
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

package local

import (
	"bytes"
	"html/template"
)

const defaultIstioIngress = `
apiVersion: v1
kind: Pod
metadata:
  labels:
    istio: ingressgateway
  name: dummy-default-ingressgateway-pod
  namespace: {{.namespace}}
spec:
  containers:
    - args:
      name: istio-proxy
---
apiVersion: v1
kind: Service
metadata:
  name: dummy-default-ingressgateway-service
  namespace: {{.namespace}}
spec:
  ports:
  - name: http2
    nodePort: 31380
    port: 80
    protocol: TCP
    targetPort: 80
  - name: https
    nodePort: 31390
    port: 443
    protocol: TCP
    targetPort: 443
  - name: tcp
    nodePort: 31400
    port: 31400
    protocol: TCP
    targetPort: 31400
  - name: tls
    nodePort: 31447
    port: 15443
    protocol: TCP
    targetPort: 15443
  selector:
    istio: ingressgateway
`

func getDefaultIstioIngress(namespace string) (string, error) {
	result, err := generate(defaultIstioIngress, map[string]string{"namespace": namespace})
	if err != nil {
		return "", err
	}

	return result, nil
}

func generate(tmpl string, params map[string]string) (string, error) {
	t := template.Must(template.New("code").Parse(tmpl))

	var b bytes.Buffer
	if err := t.Execute(&b, params); err != nil {
		return "", err
	}
	return b.String(), nil
}
