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

package istio

import (
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
)

func TestSplitIstioYaml(t *testing.T) {
	var tests = []struct {
		name       string
		input      string
		deployment string
		config     string
	}{
		{
			name: "deployment only",
			input: `
---
# Source: istio/templates/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: istio-multi
  namespace: istio-system
---

---
# Source: istio/charts/prometheus/templates/clusterrole.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: prometheus-istio-system
rules:
- apiGroups: [""]
  resources:
  - nodes
---
apiVersion: v1
kind: Service
metadata:
  name: istio-telemetry
  namespace: istio-system
  annotations:
   networking.istio.io/exportTo: "*"
  labels:
    app: mixer
spec:
  ports:
  - name: grpc-mixer
    port: 9091
  selector:
    istio: mixer
    istio-mixer-type: telemetry
---
`,
			deployment: `
# Source: istio/templates/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: istio-multi
  namespace: istio-system
---

---
# Source: istio/charts/prometheus/templates/clusterrole.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: prometheus-istio-system
rules:
- apiGroups: [""]
  resources:
  - nodes
---
apiVersion: v1
kind: Service
metadata:
  name: istio-telemetry
  namespace: istio-system
  annotations:
   networking.istio.io/exportTo: "*"
  labels:
    app: mixer
spec:
  ports:
  - name: grpc-mixer
    port: 9091
  selector:
    istio: mixer
    istio-mixer-type: telemetry
---
`,
			config: ``,
		},
		{
			name: "config only",
			input: `

---
# Source: istio/charts/mixer/templates/config.yaml

apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istioproxy
  namespace: istio-system
  labels:
    app: mixer
    chart: mixer
    heritage: Tiller
    release: istio
spec:
  attributes:
    origin.ip:
      valueType: IP_ADDRESS
    origin.uid:
      valueType: STRING
    origin.user:
      valueType: STRING
    quota.cache_hit:
      valueType: BOOL

---
# Configuration needed by Mixer.
# Mixer cluster is delivered via CDS
# Specify mixer cluster settings
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-policy
  namespace: istio-system
  labels:
    app: mixer
    chart: mixer
    heritage: Tiller
    release: istio
spec:
  host: istio-policy.istio-system.svc.cluster.local
  trafficPolicy:
    connectionPool:
      http:
        http2MaxRequests: 10000
        maxRequestsPerConnection: 10000
`,
			deployment: ``,
			config: `
# Source: istio/charts/mixer/templates/config.yaml

apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istioproxy
  namespace: istio-system
  labels:
    app: mixer
    chart: mixer
    heritage: Tiller
    release: istio
spec:
  attributes:
    origin.ip:
      valueType: IP_ADDRESS
    origin.uid:
      valueType: STRING
    origin.user:
      valueType: STRING
    quota.cache_hit:
      valueType: BOOL

---
# Configuration needed by Mixer.
# Mixer cluster is delivered via CDS
# Specify mixer cluster settings
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-policy
  namespace: istio-system
  labels:
    app: mixer
    chart: mixer
    heritage: Tiller
    release: istio
spec:
  host: istio-policy.istio-system.svc.cluster.local
  trafficPolicy:
    connectionPool:
      http:
        http2MaxRequests: 10000
        maxRequestsPerConnection: 10000
`,
		},
		{
			name: "mixed",
			input: `

---
# Source: istio/charts/mixer/templates/config.yaml

apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istioproxy
  namespace: istio-system
  labels:
    app: mixer
    chart: mixer
    heritage: Tiller
    release: istio
spec:
  attributes:
    origin.ip:
      valueType: IP_ADDRESS
    origin.uid:
      valueType: STRING
    origin.user:
      valueType: STRING
    quota.cache_hit:
      valueType: BOOL

---
# Source: istio/templates/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: istio-multi
  namespace: istio-system
---
# Configuration needed by Mixer.
# Mixer cluster is delivered via CDS
# Specify mixer cluster settings
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-policy
  namespace: istio-system
  labels:
    app: mixer
    chart: mixer
    heritage: Tiller
    release: istio
spec:
  host: istio-policy.istio-system.svc.cluster.local
  trafficPolicy:
    connectionPool:
      http:
        http2MaxRequests: 10000
        maxRequestsPerConnection: 10000
---
# Source: istio/templates/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: istio-multi
  namespace: istio-system
---
`,
			deployment: `
---
# Source: istio/templates/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: istio-multi
  namespace: istio-system
---
# Source: istio/templates/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: istio-multi
  namespace: istio-system
---
`,
			config: `
# Source: istio/charts/mixer/templates/config.yaml

apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istioproxy
  namespace: istio-system
  labels:
    app: mixer
    chart: mixer
    heritage: Tiller
    release: istio
spec:
  attributes:
    origin.ip:
      valueType: IP_ADDRESS
    origin.uid:
      valueType: STRING
    origin.user:
      valueType: STRING
    quota.cache_hit:
      valueType: BOOL

---
# Configuration needed by Mixer.
# Mixer cluster is delivered via CDS
# Specify mixer cluster settings
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-policy
  namespace: istio-system
  labels:
    app: mixer
    chart: mixer
    heritage: Tiller
    release: istio
spec:
  host: istio-policy.istio-system.svc.cluster.local
  trafficPolicy:
    connectionPool:
      http:
        http2MaxRequests: 10000
        maxRequestsPerConnection: 10000
`,
		},
		{
			name: "configmap with istio payload",
			input: `
# Source: istio/charts/security/templates/configmap.yaml
apiVersion: v1	
kind: ConfigMap	
metadata:	
  name: istio-security-custom-resources	
  namespace: istio-system	
  labels:	
    app: security	
    chart: security	
    heritage: Tiller	
    release: istio	
    istio: citadel	
data:	
  custom-resources.yaml: |-    
    # Authentication policy to enable permissive mode for all services (that have sidecar) in the mesh.
    apiVersion: "authentication.istio.io/v1alpha1"
    kind: "MeshPolicy"
    metadata:
      name: "default"
      labels:
        app: security
        chart: security
        heritage: Tiller
        release: istio
    spec:
      peers:
      - mtls:
          mode: PERMISSIVE	
  run.sh: |-    
    #!/bin/sh
    
    set -x
    
    if [ "$#" -ne "1" ]; then
        echo "first argument should be path to custom resource yaml"
        exit 1
    fi
    
    pathToResourceYAML=${1}
    
    kubectl get validatingwebhookconfiguration istio-galley 2>/dev/null
    if [ "$?" -eq 0 ]; then
        echo "istio-galley validatingwebhookconfiguration found - waiting for istio-galley deployment to be ready"
        while true; do
            kubectl -n istio-system get deployment istio-galley 2>/dev/null
            if [ "$?" -eq 0 ]; then
                break
            fi
            sleep 1
        done
        kubectl -n istio-system rollout status deployment istio-galley
        if [ "$?" -ne 0 ]; then
            echo "istio-galley deployment rollout status check failed"
            exit 1
        fi
        echo "istio-galley deployment ready for configuration validation"
    fi
    sleep 5
    kubectl apply -f ${pathToResourceYAML}
`,
			deployment: `
# Source: istio/charts/security/templates/configmap.yaml
apiVersion: v1	
kind: ConfigMap	
metadata:	
  name: istio-security-custom-resources	
  namespace: istio-system	
  labels:	
    app: security	
    chart: security	
    heritage: Tiller	
    release: istio	
    istio: citadel	
data:	
  custom-resources.yaml: |-    
    # Authentication policy to enable permissive mode for all services (that have sidecar) in the mesh.
    apiVersion: "authentication.istio.io/v1alpha1"
    kind: "MeshPolicy"
    metadata:
      name: "default"
      labels:
        app: security
        chart: security
        heritage: Tiller
        release: istio
    spec:
      peers:
      - mtls:
          mode: PERMISSIVE	
  run.sh: |-    
    #!/bin/sh
    
    set -x
    
    if [ "$#" -ne "1" ]; then
        echo "first argument should be path to custom resource yaml"
        exit 1
    fi
    
    pathToResourceYAML=${1}
    
    kubectl get validatingwebhookconfiguration istio-galley 2>/dev/null
    if [ "$?" -eq 0 ]; then
        echo "istio-galley validatingwebhookconfiguration found - waiting for istio-galley deployment to be ready"
        while true; do
            kubectl -n istio-system get deployment istio-galley 2>/dev/null
            if [ "$?" -eq 0 ]; then
                break
            fi
            sleep 1
        done
        kubectl -n istio-system rollout status deployment istio-galley
        if [ "$?" -ne 0 ]; then
            echo "istio-galley deployment rollout status check failed"
            exit 1
        fi
        echo "istio-galley deployment ready for configuration validation"
    fi
    sleep 5
    kubectl apply -f ${pathToResourceYAML}
   
`,
			config: ``,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			ad, ac := splitIstioYaml(tst.input)
			if strings.TrimSpace(ad) != strings.TrimSpace(tst.deployment) {
				t.Fatalf("Deployment mismatch: \n%s", getDiff(t, ad, tst.deployment))
			}
			if strings.TrimSpace(ac) != strings.TrimSpace(tst.config) {
				t.Fatalf("Config mismatch: \n%s", getDiff(t, ac, tst.config))
			}
		})
	}
}

func getDiff(t *testing.T, actual, expected string) string {
	d := difflib.UnifiedDiff{
		FromFile: "Expected",
		A:        difflib.SplitLines(strings.TrimSpace(expected)),
		ToFile:   "Actual",
		B:        difflib.SplitLines(strings.TrimSpace(actual)),
	}
	s, err := difflib.GetUnifiedDiffString(d)
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	return s
}
