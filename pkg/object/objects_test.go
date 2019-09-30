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

package object

import (
	"strings"
	"testing"

	"istio.io/operator/pkg/util"
)

func TestHash(t *testing.T) {
	hashTests := []struct {
		desc      string
		kind      string
		namespace string
		name      string
		want      string
	}{
		{"CalculateHashForObjectWithNormalCharacter", "Service", "default", "ingressgateway", "Service:default:ingressgateway"},
		{"CalculateHashForObjectWithDash", "Deployment", "istio-system", "istio-pilot", "Deployment:istio-system:istio-pilot"},
		{"CalculateHashForObjectWithDot", "ConfigMap", "istio-system", "my.config", "ConfigMap:istio-system:my.config"},
	}

	for _, tt := range hashTests {
		t.Run(tt.desc, func(t *testing.T) {
			got := Hash(tt.kind, tt.namespace, tt.name)
			if got != tt.want {
				t.Errorf("Hash(%s): got %s for kind %s, namespace %s, name %s, want %s", tt.desc, got, tt.kind, tt.namespace, tt.name, tt.want)
			}
		})
	}
}

func TestHashNameKind(t *testing.T) {
	hashNameKindTests := []struct {
		desc string
		kind string
		name string
		want string
	}{
		{"CalculateHashNameKindForObjectWithNormalCharacter", "Service", "ingressgateway", "Service:ingressgateway"},
		{"CalculateHashNameKindForObjectWithDash", "Deployment", "istio-pilot", "Deployment:istio-pilot"},
		{"CalculateHashNameKindForObjectWithDot", "ConfigMap", "my.config", "ConfigMap:my.config"},
	}

	for _, tt := range hashNameKindTests {
		t.Run(tt.desc, func(t *testing.T) {
			got := HashNameKind(tt.kind, tt.name)
			if got != tt.want {
				t.Errorf("HashNameKind(%s): got %s for kind %s, name %s, want %s", tt.desc, got, tt.kind, tt.name, tt.want)
			}
		})
	}
}

func TestParseJSONToK8sObject(t *testing.T) {
	testDeploymentJSON := `{
	"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "istio-citadel",
		"namespace": "istio-system",
		"labels": {
			"istio": "citadel"
		}
	},
	"spec": {
		"replicas": 1,
		"selector": {
			"matchLabels": {
				"istio": "citadel"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"istio": "citadel"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "citadel",
						"image": "docker.io/istio/citadel:1.1.8",
						"args": [
							"--append-dns-names=true",
							"--grpc-port=8060",
							"--grpc-hostname=citadel",
							"--citadel-storage-namespace=istio-system",
							"--custom-dns-names=istio-pilot-service-account.istio-system:istio-pilot.istio-system",
							"--monitoring-port=15014",
							"--self-signed-ca=true"
					  ]
					}
				]
			}
		}
	}
}`
	testPodJSON := `{
	"apiVersion": "v1",
	"kind": "Pod",
	"metadata": {
		"name": "istio-galley-75bcd59768-hpt5t",
		"namespace": "istio-system",
		"labels": {
			"istio": "galley"
		}
	},
	"spec": {
		"containers": [
			{
				"name": "galley",
				"image": "docker.io/istio/galley:1.1.8",
				"command": [
					"/usr/local/bin/galley",
					"server",
					"--meshConfigFile=/etc/mesh-config/mesh",
					"--livenessProbeInterval=1s",
					"--livenessProbePath=/healthliveness",
					"--readinessProbePath=/healthready",
					"--readinessProbeInterval=1s",
					"--deployment-namespace=istio-system",
					"--insecure=true",
					"--validation-webhook-config-file",
					"/etc/config/validatingwebhookconfiguration.yaml",
					"--monitoringPort=15014",
					"--log_output_level=default:info"
				],
				"ports": [
					{
						"containerPort": 443,
						"protocol": "TCP"
					},
					{
						"containerPort": 15014,
						"protocol": "TCP"
					},
					{
						"containerPort": 9901,
						"protocol": "TCP"
					}
				]
			}
		]
	}
}`
	testServiceJSON := `{
	"apiVersion": "v1",
	"kind": "Service",
	"metadata": {
			"labels": {
					"app": "pilot"
			},
			"name": "istio-pilot",
			"namespace": "istio-system"
	},
	"spec": {
			"clusterIP": "10.102.230.31",
			"ports": [
					{
							"name": "grpc-xds",
							"port": 15010,
							"protocol": "TCP",
							"targetPort": 15010
					},
					{
							"name": "https-xds",
							"port": 15011,
							"protocol": "TCP",
							"targetPort": 15011
					},
					{
							"name": "http-legacy-discovery",
							"port": 8080,
							"protocol": "TCP",
							"targetPort": 8080
					},
					{
							"name": "http-monitoring",
							"port": 15014,
							"protocol": "TCP",
							"targetPort": 15014
					}
			],
			"selector": {
					"istio": "pilot"
			},
			"sessionAffinity": "None",
			"type": "ClusterIP"
	}
}`

	parseJSONToK8sObjectTests := []struct {
		desc          string
		objString     string
		wantGroup     string
		wantKind      string
		wantName      string
		wantNamespace string
	}{
		{"ParseJsonToK8sDeployment", testDeploymentJSON, "apps", "Deployment", "istio-citadel", "istio-system"},
		{"ParseJsonToK8sPod", testPodJSON, "", "Pod", "istio-galley-75bcd59768-hpt5t", "istio-system"},
		{"ParseJsonToK8sService", testServiceJSON, "", "Service", "istio-pilot", "istio-system"},
	}

	for _, tt := range parseJSONToK8sObjectTests {
		t.Run(tt.desc, func(t *testing.T) {
			k8sObj, err := ParseJSONToK8sObject([]byte(tt.objString))
			if err != nil {
				k8sObjStr, err := k8sObj.YAMLDebugString()
				if err != nil {
					if k8sObj.Group != tt.wantGroup {
						t.Errorf("ParseJsonToK8sObject(%s): got group %s for k8s object %s, want %s", tt.desc, k8sObj.Group, k8sObjStr, tt.wantGroup)
					}
					if k8sObj.Group != tt.wantGroup {
						t.Errorf("ParseJsonToK8sObject(%s): got kind %s for k8s object %s, want %s", tt.desc, k8sObj.Kind, k8sObjStr, tt.wantKind)
					}
					if k8sObj.Name != tt.wantName {
						t.Errorf("ParseJsonToK8sObject(%s): got name %s for k8s object %s, want %s", tt.desc, k8sObj.Name, k8sObjStr, tt.wantName)
					}
					if k8sObj.Namespace != tt.wantNamespace {
						t.Errorf("ParseJsonToK8sObject(%s): got group %s for k8s object %s, want %s", tt.desc, k8sObj.Namespace, k8sObjStr, tt.wantNamespace)
					}
				}
			}
		})
	}
}

func TestParseYAMLToK8sObject(t *testing.T) {
	testDeploymentYaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
        args:
        - "--append-dns-names=true"
        - "--grpc-port=8060"
        - "--grpc-hostname=citadel"
        - "--citadel-storage-namespace=istio-system"
        - "--custom-dns-names=istio-pilot-service-account.istio-system:istio-pilot.istio-system"
        - "--monitoring-port=15014"
        - "--self-signed-ca=true"`

	testPodYaml := `apiVersion: v1
kind: Pod
metadata:
  name: istio-galley-75bcd59768-hpt5t
  namespace: istio-system
  labels:
    istio: galley
spec:
  containers:
  - name: galley
    image: docker.io/istio/galley:1.1.8
    command:
    - "/usr/local/bin/galley"
    - server
    - "--meshConfigFile=/etc/mesh-config/mesh"
    - "--livenessProbeInterval=1s"
    - "--livenessProbePath=/healthliveness"
    - "--readinessProbePath=/healthready"
    - "--readinessProbeInterval=1s"
    - "--deployment-namespace=istio-system"
    - "--insecure=true"
    - "--validation-webhook-config-file"
    - "/etc/config/validatingwebhookconfiguration.yaml"
    - "--monitoringPort=15014"
    - "--log_output_level=default:info"
    ports:
    - containerPort: 443
      protocol: TCP
    - containerPort: 15014
      protocol: TCP
    - containerPort: 9901
      protocol: TCP`

	testServiceYaml := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-pilot
  namespace: istio-system
spec:
  clusterIP: 10.102.230.31
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: https-xds
    port: 15011
    protocol: TCP
    targetPort: 15011
  - name: http-legacy-discovery
    port: 8080
    protocol: TCP
    targetPort: 8080
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
  sessionAffinity: None
  type: ClusterIP`

	parseYAMLToK8sObjectTests := []struct {
		desc          string
		objString     string
		wantGroup     string
		wantKind      string
		wantName      string
		wantNamespace string
	}{
		{"ParseYamlToK8sDeployment", testDeploymentYaml, "apps", "Deployment", "istio-citadel", "istio-system"},
		{"ParseYamlToK8sPod", testPodYaml, "", "Pod", "istio-galley-75bcd59768-hpt5t", "istio-system"},
		{"ParseYamlToK8sService", testServiceYaml, "", "Service", "istio-pilot", "istio-system"},
	}

	for _, tt := range parseYAMLToK8sObjectTests {
		t.Run(tt.desc, func(t *testing.T) {
			k8sObj, err := ParseYAMLToK8sObject([]byte(tt.objString))
			if err != nil {
				k8sObjStr, err := k8sObj.YAMLDebugString()
				if err != nil {
					if k8sObj.Group != tt.wantGroup {
						t.Errorf("ParseYAMLToK8sObject(%s): got group %s for k8s object %s, want %s", tt.desc, k8sObj.Group, k8sObjStr, tt.wantGroup)
					}
					if k8sObj.Group != tt.wantGroup {
						t.Errorf("ParseYAMLToK8sObject(%s): got kind %s for k8s object %s, want %s", tt.desc, k8sObj.Kind, k8sObjStr, tt.wantKind)
					}
					if k8sObj.Name != tt.wantName {
						t.Errorf("ParseYAMLToK8sObject(%s): got name %s for k8s object %s, want %s", tt.desc, k8sObj.Name, k8sObjStr, tt.wantName)
					}
					if k8sObj.Namespace != tt.wantNamespace {
						t.Errorf("ParseYAMLToK8sObject(%s): got group %s for k8s object %s, want %s", tt.desc, k8sObj.Namespace, k8sObjStr, tt.wantNamespace)
					}
				}
			}
		})
	}
}

func TestParseK8sObjectsFromYAMLManifest(t *testing.T) {
	testDeploymentYaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
        args:
        - "--append-dns-names=true"
        - "--grpc-port=8060"
        - "--grpc-hostname=citadel"
        - "--citadel-storage-namespace=istio-system"
        - "--custom-dns-names=istio-pilot-service-account.istio-system:istio-pilot.istio-system"
        - "--monitoring-port=15014"
        - "--self-signed-ca=true"`

	testPodYaml := `apiVersion: v1
kind: Pod
metadata:
  name: istio-galley-75bcd59768-hpt5t
  namespace: istio-system
  labels:
    istio: galley
spec:
  containers:
  - name: galley
    image: docker.io/istio/galley:1.1.8
    command:
    - "/usr/local/bin/galley"
    - server
    - "--meshConfigFile=/etc/mesh-config/mesh"
    - "--livenessProbeInterval=1s"
    - "--livenessProbePath=/healthliveness"
    - "--readinessProbePath=/healthready"
    - "--readinessProbeInterval=1s"
    - "--deployment-namespace=istio-system"
    - "--insecure=true"
    - "--validation-webhook-config-file"
    - "/etc/config/validatingwebhookconfiguration.yaml"
    - "--monitoringPort=15014"
    - "--log_output_level=default:info"
    ports:
    - containerPort: 443
      protocol: TCP
    - containerPort: 15014
      protocol: TCP
    - containerPort: 9901
      protocol: TCP`

	testServiceYaml := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-pilot
  namespace: istio-system
spec:
  clusterIP: 10.102.230.31
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: https-xds
    port: 15011
    protocol: TCP
    targetPort: 15011
  - name: http-legacy-discovery
    port: 8080
    protocol: TCP
    targetPort: 8080
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
  sessionAffinity: None
  type: ClusterIP`

	parseK8sObjectsFromYAMLManifestTests := []struct {
		desc    string
		objsMap map[string]string
	}{
		{
			"FromHybridYAMLManifest",
			map[string]string{
				"Deployment:istio-system:istio-citadel":          testDeploymentYaml,
				"Pod:istio-system:istio-galley-75bcd59768-hpt5t": testPodYaml,
				"Service:istio-system:istio-pilot":               testServiceYaml,
			},
		},
	}

	for _, tt := range parseK8sObjectsFromYAMLManifestTests {
		t.Run(tt.desc, func(t *testing.T) {
			testManifestYaml := strings.Join([]string{testDeploymentYaml, testPodYaml, testServiceYaml}, YAMLSeparator)
			gotK8sObjs, err := ParseK8sObjectsFromYAMLManifest(testManifestYaml)
			if err != nil {
				gotK8sObjsMap := gotK8sObjs.ToMap()
				for objHash, want := range tt.objsMap {
					if gotObj, ok := gotK8sObjsMap[objHash]; ok {
						gotObjYaml, err := gotObj.YAMLDebugString()
						if err != nil {
							if !util.IsYAMLEqual(gotObjYaml, want) {
								t.Errorf("ParseK8sObjectsFromYAMLManifest(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, gotObjYaml, want, util.YAMLDiff(gotObjYaml, want))
							}
						}
					} else {
						t.Errorf("ParseK8sObjectsFromYAMLManifest(%s): the k8s object map from %s should contains object with hash %s", tt.desc, testManifestYaml, objHash)
					}
				}
			}
		})
	}
}

func TestManifestDiff(t *testing.T) {
	testDeploymentYaml1 := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
`

	testDeploymentYaml2 := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.2.2
`

	testPodYaml1 := `apiVersion: v1
kind: Pod
metadata:
  name: istio-galley-75bcd59768-hpt5t
  namespace: istio-system
  labels:
    istio: galley
spec:
  containers:
  - name: galley
    image: docker.io/istio/galley:1.1.8
    ports:
    - containerPort: 443
      protocol: TCP
    - containerPort: 15014
      protocol: TCP
    - containerPort: 9901
      protocol: TCP
`

	testServiceYaml1 := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-pilot
  namespace: istio-system
spec:
  type: ClusterIP
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
`

	manifestDiffTests := []struct {
		desc        string
		yamlStringA string
		yamlStringB string
		want        string
	}{
		{
			"ManifestDiffWithIdenticalResource",
			testDeploymentYaml1 + YAMLSeparator,
			testDeploymentYaml1,
			"",
		},
		{
			"ManifestDiffWithIdenticalMultipleResources",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testPodYaml1 + YAMLSeparator + testServiceYaml1 + YAMLSeparator + testDeploymentYaml1,
			"",
		},
		{
			"ManifestDiffWithDifferentResource",
			testDeploymentYaml1,
			testDeploymentYaml2,
			"Object Deployment:istio-system:istio-citadel has diffs",
		},
		{
			"ManifestDiffWithDifferentMultipleResources",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testDeploymentYaml2 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			"Object Deployment:istio-system:istio-citadel has diffs",
		},
		{
			"ManifestDiffMissingResourcesInA",
			testPodYaml1 + YAMLSeparator + testDeploymentYaml1 + YAMLSeparator,
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			"Object Service:istio-system:istio-pilot is missing in A",
		},
		{
			"ManifestDiffMissingResourcesInB",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testServiceYaml1 + YAMLSeparator + testPodYaml1,
			"Object Deployment:istio-system:istio-citadel is missing in B",
		},
	}

	for _, tt := range manifestDiffTests {
		for _, v := range []bool{true, false} {
			t.Run(tt.desc, func(t *testing.T) {
				got, err := ManifestDiff(tt.yamlStringA, tt.yamlStringB, v)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.Contains(got, tt.want) {
					t.Errorf("%s:\ngot:\n%v\ndoes't contains\nwant:\n%v", tt.desc, got, tt.want)
				}
			})
		}
	}
}

func TestManifestDiffWithSelectAndIgnore(t *testing.T) {
	testDeploymentYaml1 := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
---
`

	testDeploymentYaml2 := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.2.2
---
`

	testPodYaml1 := `apiVersion: v1
kind: Pod
metadata:
  name: istio-galley-75bcd59768-hpt5t
  namespace: istio-system
  labels:
    istio: galley
spec:
  containers:
  - name: galley
    image: docker.io/istio/galley:1.1.8
    ports:
    - containerPort: 443
      protocol: TCP
    - containerPort: 15014
      protocol: TCP
    - containerPort: 9901
      protocol: TCP
---
`

	testServiceYaml1 := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-pilot
  namespace: istio-system
spec:
  type: ClusterIP
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
---
`

	manifestDiffWithSelectAndIgnoreTests := []struct {
		desc            string
		yamlStringA     string
		yamlStringB     string
		selectResources string
		ignoreResources string
		want            string
	}{
		{
			"ManifestDiffWithSelectAndIgnoreForIdenticalResource",
			testDeploymentYaml1 + YAMLSeparator,
			testDeploymentYaml1,
			"::",
			"",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourcesIgnoreSingle",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testDeploymentYaml1 + YAMLSeparator,
			"Deployment:*:istio-citadel",
			"Service:*:",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourcesIgnoreMultiple",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testDeploymentYaml1,
			"Deployment:*:istio-citadel",
			"Pod::*,Service:istio-system:*",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourcesSelectSingle",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testServiceYaml1 + YAMLSeparator + testDeploymentYaml1,
			"Deployment::istio-citadel",
			"Pod:*:*",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourcesSelectSingle",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testServiceYaml1 + YAMLSeparator + testDeploymentYaml1,
			"Deployment::istio-citadel,Service:istio-system:istio-pilot,Pod:*:*",
			"Pod:*:*",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourceForDefault",
			testDeploymentYaml1,
			testDeploymentYaml2 + YAMLSeparator,
			"::",
			"",
			"Object Deployment:istio-system:istio-citadel has diffs",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourceForSingleSelectAndIgnore",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testDeploymentYaml2 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			"Deployment:*:*",
			"Pod:*:*",
			"Object Deployment:istio-system:istio-citadel has diffs",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForMissingResourcesInA",
			testPodYaml1 + YAMLSeparator + testDeploymentYaml1,
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			"Pod:istio-system:Citadel,Service:istio-system:",
			"Pod:*:*",
			"Object Service:istio-system:istio-pilot is missing in A",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForMissingResourcesInB",
			testDeploymentYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator + testServiceYaml1,
			testServiceYaml1 + YAMLSeparator + testPodYaml1 + YAMLSeparator,
			"*:istio-system:*",
			"Pod::",
			"Object Deployment:istio-system:istio-citadel is missing in B",
		},
	}

	for _, tt := range manifestDiffWithSelectAndIgnoreTests {
		for _, v := range []bool{true, false} {
			t.Run(tt.desc, func(t *testing.T) {
				got, err := ManifestDiffWithRenameSelectIgnore(tt.yamlStringA, tt.yamlStringB,
					"", tt.selectResources, tt.ignoreResources, v)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.Contains(got, tt.want) {
					t.Errorf("%s:\ngot:\n%v\ndoes't contains\nwant:\n%v", tt.desc, got, tt.want)
				}
			})
		}
	}
}

func TestManifestDiffWithRenameSelectIgnore(t *testing.T) {
	testDeploymentYaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
---
`

	testDeploymentYamlRenamed := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-ca
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
---
`

	testServiceYaml := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-pilot
  namespace: istio-system
spec:
  type: ClusterIP
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
---
`

	testServiceYamlRenamed := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-control
  namespace: istio-system
spec:
  type: ClusterIP
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
---
`

	manifestDiffWithRenameSelectIgnoreTests := []struct {
		desc            string
		yamlStringA     string
		yamlStringB     string
		renameResources string
		selectResources string
		ignoreResources string
		want            string
	}{
		{
			"ManifestDiffDeployWithRenamedFlagMultiResourceWildcard",
			testDeploymentYaml + YAMLSeparator + testServiceYaml,
			testDeploymentYamlRenamed + YAMLSeparator + testServiceYamlRenamed,
			"Service:*:istio-pilot->::istio-control,Deployment::istio-citadel->::istio-ca",
			"::",
			"",
			`

Object Deployment:istio-system:istio-ca has diffs:

metadata:
  name: istio-citadel -> istio-ca


Object Service:istio-system:istio-control has diffs:

metadata:
  name: istio-pilot -> istio-control
`,
		},
		{
			"ManifestDiffDeployWithRenamedFlagMultiResource",
			testDeploymentYaml + YAMLSeparator + testServiceYaml,
			testDeploymentYamlRenamed + YAMLSeparator + testServiceYamlRenamed,
			"Service:istio-system:istio-pilot->Service:istio-system:istio-control,Deployment:istio-system:istio-citadel->Deployment:istio-system:istio-ca",
			"::",
			"",
			`

Object Deployment:istio-system:istio-ca has diffs:

metadata:
  name: istio-citadel -> istio-ca


Object Service:istio-system:istio-control has diffs:

metadata:
  name: istio-pilot -> istio-control
`,
		},
		{
			"ManifestDiffDeployWithRenamedFlag",
			testDeploymentYaml,
			testDeploymentYamlRenamed,
			"Deployment:istio-system:istio-citadel->Deployment:istio-system:istio-ca",
			"::",
			"",
			`

Object Deployment:istio-system:istio-ca has diffs:

metadata:
  name: istio-citadel -> istio-ca
`,
		},
		{
			"ManifestDiffRenamedDeploy",
			testDeploymentYaml,
			testDeploymentYamlRenamed,
			"",
			"::",
			"",
			`

Object Deployment:istio-system:istio-ca is missing in A:



Object Deployment:istio-system:istio-citadel is missing in B:

`,
		},
	}

	for _, tt := range manifestDiffWithRenameSelectIgnoreTests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := ManifestDiffWithRenameSelectIgnore(tt.yamlStringA, tt.yamlStringB,
				tt.renameResources, tt.selectResources, tt.ignoreResources, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("%s:\ngot:\n%v\ndoes't equals to\nwant:\n%v", tt.desc, got, tt.want)
			}
		})
	}
}
