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

import ()

// TODO: Parameterize namespace?
const defaultIstioIngress = `
apiVersion: v1
kind: Pod
metadata:
  labels:
    istio: ingressgateway
  name: dummy-default-ingressgateway-pod
  namespace: istio-system
spec:
  containers:
    - args:
      name: istio-proxy
---
apiVersion: v1
kind: Service
metadata:
  name: dummy-default-ingressgateway-service
  namespace: istio-system
spec:
  ports:
  - name: http2
    nodePort: 31380
    port: 80
    protocol: TCP
    targetPort: 80
  selector:
    istio: ingressgateway
`
