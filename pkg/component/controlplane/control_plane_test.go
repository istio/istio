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

package controlplane

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/object"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/version"
)

const (
	baseValues = `
values:
  global:
    # TODO: this should be derived
    configNamespace: istio-control
    defaultPodDisruptionBudget:
      enabled: false
    mtls:
      enabled: false
    logging:
      level: "default:info"
    k8sIngress:
      enabled: false
      gatewayName: ingressgateway
      enableHttps: false
    proxy_init:
      image: proxy_init
    imagePullPolicy: Always
    controlPlaneSecurityEnabled: true
    disablePolicyChecks: true
    policyCheckFailOpen: false
    enableTracing: true
    mtls:
      enabled: false
    arch:
      amd64: 2
      s390x: 2
      ppc64le: 2
    oneNamespace: false
    configValidation: true
    defaultResources:
      requests:
        cpu: 10m
    defaultPodDisruptionBudget:
      enabled: true
    useMCP: true
    outboundTrafficPolicy:
      mode: ALLOW_ANY
    # TODO: derive from operator API version.
    version: ""
    prometheusNamespace: istio-telemetry
    # TODO: remove requirement to set these to nil values. This is an issue with helm charts.
    meshExpansion: {}
    multiCluster: {}
    sds:
      enabled: false
      udsPath: ""
      useTrustworthyJwt: false
      useNormalJwt: false
    tracer:
      lightstep:
        address: ""
        accessToken: ""
        secure: true
        cacertPath: ""
      zipkin:
        address: ""
      datadog:
        address: "$(HOST_IP):8126"

    proxy:
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

var (
	testDataDir      string
	helmChartTestDir string
	globalValuesFile string
)

func init() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	testDataDir = filepath.Join(wd, "testdata")
	helmChartTestDir = filepath.Join(testDataDir, "charts")
	globalValuesFile = filepath.Join(helmChartTestDir, "global.yaml")
}

func TestRenderInstallationSuccessV13(t *testing.T) {
	tests := []struct {
		desc        string
		installSpec string
	}{
		{
			desc: "all_off",
			installSpec: `
defaultNamespace: istio-control
trafficManagement:
  enabled: false
policy:
  enabled: false
telemetry:
  enabled: false
security:
  enabled: false
configManagement:
  enabled: false
autoInjection:
  enabled: false
`,
		},
		{
			desc: "pilot_default",
			installSpec: `
hub: docker.io/istio
tag: 1.1.4
defaultNamespace: istio-control
policy:
  enabled: false
telemetry:
  enabled: false
security:
  enabled: false
configManagement:
  enabled: false
autoInjection:
  enabled: false
trafficManagement:
  enabled: true
  components:
    proxy:
      enabled: false


`,
		},
		{
			desc: "pilot_k8s_settings",
			installSpec: `
hub: docker.io/istio
tag: 1.1.4
defaultNamespace: istio-control
policy:
  enabled: false
telemetry:
  enabled: false
security:
  enabled: false
configManagement:
  enabled: false
autoInjection:
  enabled: false
trafficManagement:
  enabled: true
  components:
    namespace: istio-control
    proxy:
      enabled: false
    pilot:
      k8s:
        env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: new.path
          - name: GODEBUG
            value: gctrace=111
          - name: NEW_VAR
            value: new_value
        hpaSpec:
          maxReplicas: 333
          minReplicas: 222
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istio-pilot
          metrics:
            - type: Resource
              resource:
                name: cpu
                targetAverageUtilization: 444
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 555
          periodSeconds: 666
          timeoutSeconds: 777
        resources:
          requests:
            cpu: 888m
            memory: 999Mi 
`,
		},
		{
			desc: "pilot_override_values",
			installSpec: `
hub: docker.io/istio
tag: 1.1.4
defaultNamespace: istio-control
policy:
  enabled: false
telemetry:
  enabled: false
security:
  enabled: false
configManagement:
  enabled: false
autoInjection:
  enabled: false
trafficManagement:
  enabled: true
  components:
    namespace: istio-control
    proxy:
      enabled: false
unvalidatedValues:
  pilot:
    replicaCount: 5
    resources:
      requests:
        cpu: 111m
        memory: 222Mi
    myCustomKey: someValue
`,
		},
		{
			desc: "pilot_override_kubernetes",
			installSpec: `
hub: docker.io/istio
tag: 1.1.4
defaultNamespace: istio-control
policy:
  enabled: false
telemetry:
  enabled: false
security:
  enabled: false
configManagement:
  enabled: false
autoInjection:
  enabled: false
trafficManagement:
  enabled: true
  components:
    proxy:
      enabled: false
    pilot:
      k8s:
        overlays:
        - kind: Deployment
          name: istio-pilot
          patches:
          - path: spec.template.spec.containers.[name:discovery].args.[30m]
            value: "60m" # OVERRIDDEN
          - path: spec.template.spec.containers.[name:discovery].ports.[containerPort:8080].containerPort
            value: 1234 # OVERRIDDEN
        - kind: Service
          name: istio-pilot
          patches:
          - path: spec.ports.[name:grpc-xds].port
            value: 11111 # OVERRIDDEN
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			var is v1alpha2.IstioControlPlaneSpec
			spec := `installPackagePath: ` + helmChartTestDir + "\n"
			spec += `profile: ` + helmChartTestDir + `/global.yaml` + "\n"
			spec += tt.installSpec
			spec += baseValues
			err := util.UnmarshalWithJSONPB(spec, &is)
			if err != nil {
				t.Fatalf("yaml.Unmarshal(%s): got error %s", tt.desc, err)
			}

			tr, err := translate.NewTranslator(version.NewMinorVersion(1, 3))
			if err != nil {
				t.Fatal(err)
			}

			ins := NewIstioControlPlane(&is, tr)
			if err = ins.Run(); err != nil {
				t.Fatal(err)
			}

			got, errs := ins.RenderManifest()
			if len(errs) != 0 {
				t.Fatal(errs.Error())
			}
			want, err := readFile(tt.desc + ".yaml")
			if err != nil {
				t.Fatal(err)
			}
			diff, err := object.ManifestDiff(manifestMapToStr(got), want)
			if err != nil {
				t.Fatal(err)
			}
			if diff != "" {
				t.Errorf("%s: got:\n%s\nwant:\n%s\n(-got, +want)\n%s\n", tt.desc, "", "", diff)
			}

		})
	}
}

func manifestMapToStr(mm name.ManifestMap) string {
	out := ""
	for _, m := range mm {
		out += m
	}
	return out
}

func readFile(path string) (string, error) {
	b, err := ioutil.ReadFile(filepath.Join(testDataDir, path))
	return string(b), err
}
