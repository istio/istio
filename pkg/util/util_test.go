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

package util

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"

	"istio.io/operator/pkg/apis/istio/v1alpha2"

	v2beta1 "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	_ "k8s.io/apimachinery/pkg/util/intstr"
)

var (
	icp = &v1alpha2.IstioControlPlaneSpec{
		DefaultNamespacePrefix: "istio-system",
		Hub:                    "docker.io/istio",
		Tag:                    "1.1.4",
		Profile:                "default",
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
							ReadinessProbe: &v1.Probe{
								Handler: v1.Handler{},
								// 	HTTPGet: &v1.HTTPGetAction{
								// 		Path: "/ready",
								// 		Port: intstr.FromInt(8080),
								// 	},
								// },
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
							"concurrency":                  2,
							"accessLogEncoding":            "TEXT",
							"logLevel":                     "warning",
							"componentLogLevel":            "misc:error",
							"dnsRefreshRate":               "300s",
							"privileged":                   false,
							"enableCoreDump":               false,
							"statusPort":                   15020,
							"readinessInitialDelaySeconds": 1,
							"readinessPeriodSeconds":       2,
							"readinessFailureThreshold":    30,
							"includeIPRanges":              "*",
							"autoInject":                   "enabled",
							"tracer":                       "zipkin",
						},
					},
				},
			},
		},
	}

	icpYaml = `
defaultNamespacePrefix: istio-system
hub: docker.io/istio
tag: 1.1.4
defaultNamespacePrefix: istio-system
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
            handler: {}
# TODO: uncomment the following lines after the jsonpb unmarshaling issue is resolved: https://github.com/istio/istio/issues/15366
#            httpGet:
#              path: /ready
#              port: 8080
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
          concurrency: 2
          accessLogEncoding: TEXT
          logLevel: warning
          componentLogLevel: "misc:error"
          dnsRefreshRate: 300s
          privileged: false
          enableCoreDump: false
          statusPort: 15020
          readinessInitialDelaySeconds: 1
          readinessPeriodSeconds: 2
          readinessFailureThreshold: 30
          includeIPRanges: "*"
          autoInject: enabled
          tracer: "zipkin"
`
)

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
			got := ToYAMLWithJSONPB(icp)
			if !IsYAMLEqual(got, tt.want) || YAMLDiff(got, tt.want) != "" {
				t.Errorf("TestToYAMLWithJSONPB(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, tt.want, YAMLDiff(got, tt.want))
			}
		})
	}
}

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
			err := UnmarshalWithJSONPB(icpYaml, got)
			if err != nil {
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("TestUnmarshalWithJSONPB(%s): got:\n%v\n\nwant:\n%v", tt.desc, got, tt.want)
				}
			}
		})
	}
}
