//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package settings

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	google_protobuf "github.com/golang/protobuf/ptypes/duration"
	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/probe"
)

func TestToProbeOptions(t *testing.T) {
	tests := []struct {
		name      string
		input     *Probe
		expected  *probe.Options
		expectErr bool
	}{
		{
			name:     "empty",
			input:    &Probe{},
			expected: &probe.Options{},
		},
		{
			name: "basic",
			input: &Probe{
				Path: "foo",
			},
			expected: &probe.Options{
				Path: "foo",
			},
		},
		{
			name: "duration",
			input: &Probe{
				Path:     "foo",
				Interval: ptypes.DurationProto(time.Second * 10),
			},
			expected: &probe.Options{
				Path:           "foo",
				UpdateInterval: time.Second * 10,
			},
		},
		{
			name: "disabled",
			input: &Probe{
				Disable:  true,
				Path:     "foo",
				Interval: ptypes.DurationProto(time.Second * 10),
			},
			expected: &probe.Options{
				Path:           "",
				UpdateInterval: 0,
			},
		},
		{
			name: "error in duration",
			input: &Probe{
				Path: "foo",
				Interval: &google_protobuf.Duration{
					Seconds: math.MaxInt64,
					Nanos:   math.MaxInt32,
				},
			},
			expectErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := ToProbeOptions(test.input)
			if test.expectErr {
				if err == nil {
					t.Fatal("Expected error not found")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}

			if !reflect.DeepEqual(actual, test.expected) {
				t.Fatalf("Mismatch: Actual:\n%+v\nExpected:\n%+v\n", actual, test.expected)
			}
		})
	}
}

func TestToServerArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *server.Args
	}{
		{
			name: "default",
			input: `
`,
			expected: &server.Args{
				APIAddress:             "tcp://0.0.0.0:9901",
				MaxReceivedMessageSize: 1024 * 1024,
				MaxConcurrentStreams:   1024,
				IntrospectionOptions:   ctrlz.DefaultOptions(),
				EnableServer:           true,
				DomainSuffix:           "cluster.local",
				MeshConfigFile:         "/etc/mesh-config/mesh",
				CredentialOptions:      creds.DefaultOptions(),
			},
		},

		{
			name: "basics",
			input: `
general:
  kubeConfig: kbcfg
  introspection:
    address: "127.0.0.1"
    port: 9876
  liveness:
    interval: 2s
    path: "/healthLiveness"
  meshConfigFile: "/etc/mesh-config/mesh"
  monitoringPort: 9093
  pprofPort: 9094
  readiness:
    interval: 2s
    path: /healthReadiness
processing:
  domainSuffix: c.l
  source:
    kubernetes:
      resyncPeriod: 1s
  server:
    address: tcp://127.0.0.1:9901
    grpcTracing: true
    maxReceivedMessageSize: 1
    maxConcurrentStreams: 2
    disableResourceReadyCheck: true
    disable: true
`,
			expected: &server.Args{
				APIAddress:                "tcp://127.0.0.1:9901",
				MaxReceivedMessageSize:    1,
				MaxConcurrentStreams:      2,
				IntrospectionOptions:      ctrlz.DefaultOptions(),
				EnableServer:              false,
				DomainSuffix:              "c.l",
				MeshConfigFile:            "/etc/mesh-config/mesh",
				Insecure:                  false,
				EnableGRPCTracing:         true,
				DisableResourceReadyCheck: true,
				CredentialOptions:         creds.DefaultOptions(),
				ResyncPeriod:              time.Second,
				KubeConfig:                "kbcfg",
			},
		},

		{
			name: "insecure-enabled",
			input: `
general:
  kubeConfig: kbcfg
  introspection:
    address: "127.0.0.1"
    port: 9876
  liveness:
    interval: 2s
    path: "/healthLiveness"
  meshConfigFile: "/etc/mesh-config/mesh"
  monitoringPort: 9093
  pprofPort: 9094
  readiness:
    interval: 2s
    path: /healthReadiness
processing:
  domainSuffix: c.l
  source:
    filesystem:
      path: "/configss"
  server:
    address: tcp://127.0.0.1:9901
    grpcTracing: true
    maxReceivedMessageSize: 1
    maxConcurrentStreams: 2
    disableResourceReadyCheck: true
`,
			expected: &server.Args{
				APIAddress:                "tcp://127.0.0.1:9901",
				MaxReceivedMessageSize:    1,
				MaxConcurrentStreams:      2,
				IntrospectionOptions:      ctrlz.DefaultOptions(),
				AccessListFile:            "",
				EnableServer:              true,
				DomainSuffix:              "c.l",
				MeshConfigFile:            "/etc/mesh-config/mesh",
				Insecure:                  false,
				EnableGRPCTracing:         true,
				DisableResourceReadyCheck: true,
				CredentialOptions:         creds.DefaultOptions(),
				KubeConfig:                "kbcfg",
				ConfigPath:                "/configss",
			},
		},

		{
			name: "secure-custom-mtls",
			input: `
general:
  kubeConfig: kbcfg
  introspection:
    address: "127.0.0.1"
    port: 9876
  liveness:
    interval: 2s
    path: "/healthLiveness"
  meshConfigFile: "/etc/mesh-config/mesh"
  monitoringPort: 9093
  pprofPort: 9094
  readiness:
    interval: 2s
    path: /healthReadiness
processing:
  domainSuffix: c.l
  source:
    filesystem:
      path: "/configss"
  server:
    auth:
      mtls:
        privateKey: "kf"
        clientCertificate: "cf"
        caCertificates: "cacf"
        accessListFile: "alf.yaml"

    address: tcp://127.0.0.1:9901
    grpcTracing: true
    maxReceivedMessageSize: 1
    maxConcurrentStreams: 2
    disableResourceReadyCheck: true
`,
			expected: &server.Args{
				APIAddress:                "tcp://127.0.0.1:9901",
				MaxReceivedMessageSize:    1,
				MaxConcurrentStreams:      2,
				IntrospectionOptions:      ctrlz.DefaultOptions(),
				AccessListFile:            "alf.yaml",
				EnableServer:              true,
				DomainSuffix:              "c.l",
				MeshConfigFile:            "/etc/mesh-config/mesh",
				Insecure:                  false,
				EnableGRPCTracing:         true,
				DisableResourceReadyCheck: true,
				CredentialOptions: &creds.Options{
					CertificateFile:   "cf",
					KeyFile:           "kf",
					CACertificateFile: "cacf",
				},
				KubeConfig: "kbcfg",
				ConfigPath: "/configss",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := Parse(strings.TrimSpace(test.input))
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			actual, err := ToServerArgs(cfg)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !reflect.DeepEqual(actual, test.expected) {
				t.Fatalf("Mismatch: got:\n%v\nwanted:\n%v\n", actual, test.expected)
			}
		})
	}
}

func TestToValidationArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *validation.WebhookParameters
	}{
		{
			name: "default",
			input: `
validation:
  webhookConfigFile: "webcfg.yaml"
`,
			expected: &validation.WebhookParameters{
				Port:                          443,
				CertFile:                      "/etc/certs/cert-chain.pem",
				KeyFile:                       "/etc/certs/key.pem",
				CACertFile:                    "/etc/certs/root-cert.pem",
				WebhookConfigFile:             "webcfg.yaml",
				DeploymentAndServiceNamespace: "istio-system",
				WebhookName:                   "istio-galley",
				DeploymentName:                "istio-galley",
				ServiceName:                   "istio-galley",
				EnableValidation:              true,
			},
		},

		{
			name: "basics",
			input: `
validation:
  deploymentName: istio-galley2
  deploymentNamespace: istio-system
  serviceName: istio-galley3
  webhookConfigFile: "webcfg.yaml"
  tls:
    caCertificates: /etc/certs/root-cert.pem
    clientCertificate: /etc/certs/cert-chain.pem
    privateKey: /etc/certs/key.pem
  webhookName: istio-galley1
  webhookPort: 443 
`,
			expected: &validation.WebhookParameters{
				Port:                          443,
				CertFile:                      "/etc/certs/cert-chain.pem",
				KeyFile:                       "/etc/certs/key.pem",
				CACertFile:                    "/etc/certs/root-cert.pem",
				WebhookConfigFile:             "webcfg.yaml",
				DeploymentAndServiceNamespace: "istio-system",
				WebhookName:                   "istio-galley1",
				DeploymentName:                "istio-galley2",
				ServiceName:                   "istio-galley3",
				EnableValidation:              true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := Parse(strings.TrimSpace(test.input))
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			actual, err := ToValidationArgs(cfg)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !reflect.DeepEqual(actual, test.expected) {
				t.Fatalf("Mismatch: got:\n%v\nwanted:\n%v\n", actual, test.expected)
			}
		})
	}
}

func TestToValidationArgs_ValidationError(t *testing.T) {
	g := Default()
	g.Validation.WebhookConfigFile = ""
	_, err := ToValidationArgs(g)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestToIntrospectionOptions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *ctrlz.Options
	}{
		{
			name:     "default",
			input:    ``,
			expected: ctrlz.DefaultOptions(),
		},
		{
			name: "disabled",
			input: `
general:
  introspection:
    address: 127.0.0.1
    port: 9876
    disable: true
`,
			expected: &ctrlz.Options{
				Port:    0,
				Address: "",
			},
		},
		{
			name: "basic",
			input: `
general:
  introspection:
    address: foo.bar.baz.com
    port: 6666
`,
			expected: &ctrlz.Options{
				Port:    6666,
				Address: "foo.bar.baz.com",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := Parse(strings.TrimSpace(test.input))
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			actual := toIntrospectionOptions(&cfg.General.Introspection)

			if !reflect.DeepEqual(actual, test.expected) {
				t.Fatalf("Mismatch: got:\n%v\nwanted:\n%v\n", actual, test.expected)
			}
		})
	}
}
