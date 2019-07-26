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
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	v2beta1 "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
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

var (
	icp = &v1alpha2.IstioControlPlaneSpec{
		DefaultNamespace: "istio-system",
		Hub:              "docker.io/istio",
		Tag:              "1.1.4",
		Profile:          "default",
		TrafficManagement: &v1alpha2.TrafficManagementFeatureSpec{
			Enabled: &types.BoolValue{Value: true},
			Components: &v1alpha2.TrafficManagementFeatureSpec_Components{
				Namespace: "istio-control",
				Pilot: &v1alpha2.PilotComponentSpec{
					Common: &v1alpha2.CommonComponentSpec{
						K8S: &v1alpha2.KubernetesResourcesSpec{
							Env: []*v1.EnvVar{
								{Name: "GODEBUG", Value: "gctrace=1"},
							},
							HpaSpec: &v2beta1.HorizontalPodAutoscalerSpec{
								MaxReplicas: 5,
								MinReplicas: proto.Int32(1),
								ScaleTargetRef: v2beta1.CrossVersionObjectReference{
									Kind:       "Deployment",
									Name:       "istio-pilot",
									APIVersion: "apps/v1",
								},
								Metrics: []v2beta1.MetricSpec{
									{
										Type:     v2beta1.ResourceMetricSourceType,
										Resource: &v2beta1.ResourceMetricSource{Name: v1.ResourceCPU, TargetAverageUtilization: proto.Int32(80)},
									},
								},
							},
							ReplicaCount: 1,
							ReadinessProbe: &v1alpha2.ReadinessProbe{
								HttpGet: &v1alpha2.HTTPGetAction{
									Path: "/ready",
									Port: v1alpha2.FromInt(8080),
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       30,
								TimeoutSeconds:      5,
							},
							Resources: &v1alpha2.Resources{
								Requests: map[string]string{
									"cpu":    "500m",
									"memory": "2048Mi",
								},
							},
						},
						Values: map[string]interface{}{
							"image":                           "pilot",
							"traceSampling":                   1.0,
							"configNamespace":                 "istio-config",
							"keepaliveMaxServerConnectionAge": "30m",
							"configMap":                       true,
							"ingress": map[string]interface{}{
								"ingressService":        "istio-ingressgateway",
								"ingressControllerMode": "OFF",
								"ingressClass":          "istio",
							},
							"telemetry": map[string]interface{}{
								"enabled": true,
							},
							"policy": map[string]interface{}{
								"enabled": false,
							},
							"useMCP": true,
						},
					},
				},
				Proxy: &v1alpha2.ProxyComponentSpec{
					Common: &v1alpha2.CommonComponentSpec{
						Values: map[string]interface{}{
							"image":         "proxyv2",
							"clusterDomain": "cluster.local",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"cpu":    "100m",
									"memory": "128Mi",
								},
								"limits": map[string]interface{}{
									"cpu":    "2000m",
									"memory": "128Mi",
								},
							},
							"accessLogEncoding": "TEXT",
							"logLevel":          "warning",
							"componentLogLevel": "misc:error",
							"dnsRefreshRate":    "300s",
							"privileged":        false,
							"enableCoreDump":    false,
							"includeIPRanges":   "*",
							"autoInject":        "enabled",
							"tracer":            "zipkin",
						},
					},
				},
			},
		},
	}

	icpYaml = `
defaultNamespace: istio-system
hub: docker.io/istio
tag: 1.1.4
defaultNamespace: istio-system
profile: default
trafficManagement:
  enabled: true
  components:
    namespace: istio-control
    pilot:
      common:
        k8s:
          env:
          - name: GODEBUG
            value: "gctrace=1"
          hpaSpec:
            maxReplicas: 5
            minReplicas: 1
            scaleTargetRef:
              apiVersion: apps/v1
              kind: Deployment
              name: istio-pilot
            metrics:
              - type: Resource
                resource:
                  name: cpu
                  targetAverageUtilization: 80
          replicaCount: 1
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 30
            timeoutSeconds: 5
          resources:
            requests:
              cpu: 500m
              memory: 2048Mi
        values:
          image: pilot
          traceSampling: 1.0
          configNamespace: istio-config
          keepaliveMaxServerConnectionAge: 30m
          configMap: true
          ingress:
            ingressService: istio-ingressgateway
            ingressControllerMode: "OFF"
            ingressClass: istio
          telemetry:
            enabled: true
          policy:
            enabled: false
          useMCP: true
    proxy:
      common:
        values:
          image: proxyv2
          clusterDomain: "cluster.local"
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 2000m
              memory: 128Mi
          accessLogEncoding: TEXT
          logLevel: warning
          componentLogLevel: "misc:error"
          dnsRefreshRate: 300s
          privileged: false
          enableCoreDump: false
          includeIPRanges: "*"
          autoInject: enabled
          tracer: "zipkin"
`
)

// TODO: this test may be redundant. It cannot exist in util because that creates a dependency on istio/v1alpha2.
func TestToYAMLWithJSONPB(t *testing.T) {
	toYAMLWithJSONPBTests := []struct {
		desc string
		pb   *v1alpha2.IstioControlPlaneSpec
		want string
	}{
		{"TranslateICPToYAMLWithJSONPB", icp, icpYaml},
	}

	for _, tt := range toYAMLWithJSONPBTests {
		t.Run(tt.desc, func(t *testing.T) {
			got := util.ToYAMLWithJSONPB(tt.pb)
			if !util.IsYAMLEqual(got, tt.want) || util.YAMLDiff(got, tt.want) != "" {
				t.Errorf("%s:\ngot:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, tt.want, util.YAMLDiff(got, tt.want))
			}
		})
	}
}

// TODO: this test may be redundant. It cannot exist in util because that creates a dependency on istio/v1alpha2.
func TestUnmarshalWithJSONPB(t *testing.T) {
	unmarshalWithJSONPBTests := []struct {
		desc string
		yaml string
		want *v1alpha2.IstioControlPlaneSpec
	}{
		{"UnmarshalWithJSONPBToYAML", icpYaml, icp},
	}

	for _, tt := range unmarshalWithJSONPBTests {
		t.Run(tt.desc, func(t *testing.T) {
			got := &v1alpha2.IstioControlPlaneSpec{}
			err := util.UnmarshalWithJSONPB(tt.yaml, got)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s:\ngot:\n%v\n\nwant:\n%v", tt.desc, got, tt.want)
			}
		})
	}
}
