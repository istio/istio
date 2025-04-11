// Copyright Istio Authors
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

package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestValidateFQDN(t *testing.T) {
	tests := []struct {
		fqdn  string
		valid bool
		name  string
	}{
		{
			fqdn:  strings.Repeat("x", 256),
			valid: false,
			name:  "long FQDN",
		},
		{
			fqdn:  "",
			valid: false,
			name:  "empty FQDN",
		},
		{
			fqdn:  "istio.io",
			valid: true,
			name:  "standard FQDN",
		},
		{
			fqdn:  "istio.io.",
			valid: true,
			name:  "unambiguous FQDN",
		},
		{
			fqdn:  "istio-pilot.istio-system.svc.cluster.local",
			valid: true,
			name:  "standard kubernetes FQDN",
		},
		{
			fqdn:  "istio-pilot.istio-system.svc.cluster.local.",
			valid: true,
			name:  "unambiguous kubernetes FQDN",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFQDN(tt.fqdn)
			valid := err == nil
			if valid != tt.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", tt.valid, valid, tt.fqdn)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	ports := map[int]bool{
		0:     false,
		65536: false,
		-1:    false,
		100:   true,
		1000:  true,
		65535: true,
	}
	for port, valid := range ports {
		if got := ValidatePort(port); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %d", got == nil, valid, got, port)
		}
	}
}

func TestValidateControlPlaneAuthPolicy(t *testing.T) {
	cases := []struct {
		name    string
		policy  meshconfig.AuthenticationPolicy
		isValid bool
	}{
		{
			name:    "invalid policy",
			policy:  -1,
			isValid: false,
		},
		{
			name:    "valid policy",
			policy:  0,
			isValid: true,
		},
		{
			name:    "valid policy",
			policy:  1,
			isValid: true,
		},
		{
			name:    "invalid policy",
			policy:  2,
			isValid: false,
		},
		{
			name:    "invalid policy",
			policy:  100,
			isValid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateControlPlaneAuthPolicy(c.policy); (got == nil) != c.isValid {
				t.Errorf("got valid=%v but wanted valid=%v: %v", got == nil, c.isValid, got)
			}
		})
	}
}

func TestValidateProxyAddress(t *testing.T) {
	addresses := map[string]bool{
		"istio-pilot:80":        true,
		"istio-pilot":           false,
		"isti..:80":             false,
		"10.0.0.100:9090":       true,
		"10.0.0.100":            false,
		"istio-pilot:port":      false,
		"istio-pilot:100000":    false,
		"[2001:db8::100]:80":    true,
		"[2001:db8::10::20]:80": false,
		"[2001:db8::100]":       false,
		"[2001:db8::100]:port":  false,
		"2001:db8::100:80":      false,
	}
	for addr, valid := range addresses {
		if got := ValidateProxyAddress(addr); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %s", got == nil, valid, got, addr)
		}
	}
}

func TestValidateDuration(t *testing.T) {
	type durationCheck struct {
		duration *durationpb.Duration
		isValid  bool
	}

	checks := []durationCheck{
		{
			duration: &durationpb.Duration{Seconds: 1},
			isValid:  true,
		},
		{
			duration: &durationpb.Duration{Seconds: 1, Nanos: -1},
			isValid:  false,
		},
		{
			duration: &durationpb.Duration{Seconds: -11, Nanos: -1},
			isValid:  false,
		},
		{
			duration: &durationpb.Duration{Nanos: 1},
			isValid:  false,
		},
		{
			duration: &durationpb.Duration{Seconds: 1, Nanos: 1},
			isValid:  false,
		},
	}

	for _, check := range checks {
		if got := ValidateDuration(check.duration); (got == nil) != check.isValid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, check.isValid, got, check.duration)
		}
	}
}

func TestValidateDrainDuration(t *testing.T) {
	type ParentDrainTime struct {
		Drain *durationpb.Duration
		Valid bool
	}

	combinations := []ParentDrainTime{
		{
			Drain: &durationpb.Duration{Seconds: 1},
			Valid: true,
		},
		{
			Drain: &durationpb.Duration{Seconds: 1, Nanos: 1000000},
			Valid: false,
		},
		{
			Drain: &durationpb.Duration{Seconds: -1},
			Valid: false,
		},
	}
	for _, combo := range combinations {
		if got := ValidateDrainDuration(combo.Drain); (got == nil) != combo.Valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for  Drain:%v",
				got == nil, combo.Valid, got, combo.Drain)
		}
	}
}

func TestValidateMeshConfigProxyConfig(t *testing.T) {
	valid := &meshconfig.ProxyConfig{
		ConfigPath:             "/etc/istio/proxy",
		BinaryPath:             "/usr/local/bin/envoy",
		DiscoveryAddress:       "istio-pilot.istio-system:15010",
		ProxyAdminPort:         15000,
		DrainDuration:          durationpb.New(45 * time.Second),
		ClusterName:            &meshconfig.ProxyConfig_ServiceCluster{ServiceCluster: "istio-proxy"},
		StatsdUdpAddress:       "istio-statsd-prom-bridge.istio-system:9125",
		EnvoyMetricsService:    &meshconfig.RemoteService{Address: "metrics-service.istio-system:15000"},
		EnvoyAccessLogService:  &meshconfig.RemoteService{Address: "accesslog-service.istio-system:15000"},
		ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_MUTUAL_TLS,
		Tracing:                nil,
		StatusPort:             15020,
		PrivateKeyProvider:     nil,
	}

	modify := func(config *meshconfig.ProxyConfig, fieldSetter func(*meshconfig.ProxyConfig)) *meshconfig.ProxyConfig {
		clone := protomarshal.Clone(config)
		fieldSetter(clone)
		return clone
	}

	cases := []struct {
		name    string
		in      *meshconfig.ProxyConfig
		isValid bool
		isWarn  bool
	}{
		{
			name:    "empty proxy config",
			in:      &meshconfig.ProxyConfig{},
			isValid: false,
		},
		{
			name:    "valid proxy config",
			in:      valid,
			isValid: true,
		},
		{
			name:    "config path invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ConfigPath = "" }),
			isValid: false,
		},
		{
			name:    "binary path invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.BinaryPath = "" }),
			isValid: false,
		},
		{
			name:    "discovery address invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.DiscoveryAddress = "10.0.0.100" }),
			isValid: false,
		},
		{
			name:    "proxy admin port invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ProxyAdminPort = 0 }),
			isValid: false,
		},
		{
			name:    "proxy admin port invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ProxyAdminPort = 65536 }),
			isValid: false,
		},
		{
			name:    "validate status port",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.StatusPort = 0 }),
			isValid: false,
		},
		{
			name:    "validate vstatus port",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.StatusPort = 65536 }),
			isValid: false,
		},
		{
			name:    "drain duration invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.DrainDuration = durationpb.New(-1 * time.Second) }),
			isValid: false,
		},
		{
			name: "service cluster invalid",
			in: modify(valid, func(c *meshconfig.ProxyConfig) {
				c.ClusterName = &meshconfig.ProxyConfig_ServiceCluster{ServiceCluster: ""}
			}),
			isValid: false,
		},
		{
			name:    "statsd udp address invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.StatsdUdpAddress = "10.0.0.100" }),
			isValid: false,
		},
		{
			name: "envoy metrics service address invalid",
			in: modify(valid, func(c *meshconfig.ProxyConfig) {
				c.EnvoyMetricsService = &meshconfig.RemoteService{Address: "metrics-service.istio-system"}
			}),
			isValid: false,
		},
		{
			name: "envoy access log service address invalid",
			in: modify(valid, func(c *meshconfig.ProxyConfig) {
				c.EnvoyAccessLogService = &meshconfig.RemoteService{Address: "accesslog-service.istio-system"}
			}),
			isValid: false,
		},
		{
			name:    "control plane auth policy invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ControlPlaneAuthPolicy = -1 }),
			isValid: false,
		},
		{
			name: "zipkin address is valid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Zipkin_{
							Zipkin: &meshconfig.Tracing_Zipkin{
								Address: "zipkin.istio-system:9411",
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "zipkin address with $(HOST_IP) is valid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Zipkin_{
							Zipkin: &meshconfig.Tracing_Zipkin{
								Address: "$(HOST_IP):9411",
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "zipkin config invalid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Zipkin_{
							Zipkin: &meshconfig.Tracing_Zipkin{
								Address: "10.0.0.100",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep config is valid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "collector.lightstep:8080",
								AccessToken: "abcdefg1234567",
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "lightstep address invalid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "10.0.0.100",
								AccessToken: "abcdefg1234567",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep address empty but lightstep access token is not",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "",
								AccessToken: "abcdefg1234567",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep address is valid but access token is empty",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "collector.lightstep:8080",
								AccessToken: "",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep access token empty but lightstep address is not",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "10.0.0.100",
								AccessToken: "",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep address and lightstep token both empty",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "",
								AccessToken: "",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "datadog without address",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Datadog_{
							Datadog: &meshconfig.Tracing_Datadog{},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "datadog with correct address",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Datadog_{
							Datadog: &meshconfig.Tracing_Datadog{
								Address: "datadog-agent:8126",
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "datadog with invalid address",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Datadog_{
							Datadog: &meshconfig.Tracing_Datadog{
								Address: "address-missing-port-number",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "custom tags with a literal value",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						CustomTags: map[string]*meshconfig.Tracing_CustomTag{
							"clusterID": {
								Type: &meshconfig.Tracing_CustomTag_Literal{
									Literal: &meshconfig.Tracing_Literal{
										Value: "cluster1",
									},
								},
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "custom tags with a nil value",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						CustomTags: map[string]*meshconfig.Tracing_CustomTag{
							"clusterID": nil,
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "private key provider with empty provider",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.PrivateKeyProvider = &meshconfig.PrivateKeyProvider{}
				},
			),
			isValid: false,
		},
		{
			name: "private key provider with cryptomb without poll_delay",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.PrivateKeyProvider = &meshconfig.PrivateKeyProvider{
						Provider: &meshconfig.PrivateKeyProvider_Cryptomb{
							Cryptomb: &meshconfig.PrivateKeyProvider_CryptoMb{},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "private key provider with cryptomb zero poll_delay",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.PrivateKeyProvider = &meshconfig.PrivateKeyProvider{
						Provider: &meshconfig.PrivateKeyProvider_Cryptomb{
							Cryptomb: &meshconfig.PrivateKeyProvider_CryptoMb{
								PollDelay: &durationpb.Duration{
									Seconds: 0,
									Nanos:   0,
								},
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "private key provider with cryptomb",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.PrivateKeyProvider = &meshconfig.PrivateKeyProvider{
						Provider: &meshconfig.PrivateKeyProvider_Cryptomb{
							Cryptomb: &meshconfig.PrivateKeyProvider_CryptoMb{
								PollDelay: &durationpb.Duration{
									Seconds: 0,
									Nanos:   10000,
								},
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "private key provider with qat without poll_delay",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.PrivateKeyProvider = &meshconfig.PrivateKeyProvider{
						Provider: &meshconfig.PrivateKeyProvider_Qat{
							Qat: &meshconfig.PrivateKeyProvider_QAT{},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "private key provider with qat zero poll_delay",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.PrivateKeyProvider = &meshconfig.PrivateKeyProvider{
						Provider: &meshconfig.PrivateKeyProvider_Qat{
							Qat: &meshconfig.PrivateKeyProvider_QAT{
								PollDelay: &durationpb.Duration{
									Seconds: 0,
									Nanos:   0,
								},
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "private key provider with qat",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.PrivateKeyProvider = &meshconfig.PrivateKeyProvider{
						Provider: &meshconfig.PrivateKeyProvider_Qat{
							Qat: &meshconfig.PrivateKeyProvider_QAT{
								PollDelay: &durationpb.Duration{
									Seconds: 0,
									Nanos:   1000,
								},
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "ISTIO_META_DNS_AUTO_ALLOCATE is deprecated",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					if c.ProxyMetadata == nil {
						c.ProxyMetadata = make(map[string]string)
					}
					c.ProxyMetadata["ISTIO_META_DNS_AUTO_ALLOCATE"] = "true"
				}),
			isValid: true, // allowed
			isWarn:  true, // issue a warning though
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ValidateMeshConfigProxyConfig(c.in)
			if (got.Err == nil) != c.isValid {
				if c.isValid {
					t.Errorf("got error %v, wanted none", got.Err)
				} else {
					t.Error("got no error, wanted one")
				}
			}
			if (got.Warning != nil) != c.isWarn {
				if c.isWarn {
					t.Error("got no warning, wanted one")
				} else {
					t.Errorf("got warning %v, wanted none", got.Warning)
				}
			}
		})
	}

	invalid := &meshconfig.ProxyConfig{
		ConfigPath:             "",
		BinaryPath:             "",
		DiscoveryAddress:       "10.0.0.100",
		ProxyAdminPort:         0,
		DrainDuration:          durationpb.New(-1 * time.Second),
		ClusterName:            &meshconfig.ProxyConfig_ServiceCluster{ServiceCluster: ""},
		StatsdUdpAddress:       "10.0.0.100",
		EnvoyMetricsService:    &meshconfig.RemoteService{Address: "metrics-service"},
		EnvoyAccessLogService:  &meshconfig.RemoteService{Address: "accesslog-service"},
		ControlPlaneAuthPolicy: -1,
		StatusPort:             0,
		Tracing: &meshconfig.Tracing{
			Tracer: &meshconfig.Tracing_Zipkin_{
				Zipkin: &meshconfig.Tracing_Zipkin{
					Address: "10.0.0.100",
				},
			},
		},
	}

	validation := ValidateMeshConfigProxyConfig(invalid)
	err := validation.Err
	if err == nil {
		t.Errorf("expected an error on invalid proxy mesh config: %v", invalid)
	} else {
		switch err := err.(type) {
		case *multierror.Error:
			// each field must cause an error in the field
			if len(err.Errors) != 12 {
				t.Errorf("expected an error for each field %v", err)
			}
		default:
			t.Errorf("expected a multi error as output")
		}
	}
}

func TestValidateTLS(t *testing.T) {
	testCases := []struct {
		name  string
		tls   *networking.ClientTLSSettings
		valid bool
	}{
		{
			name: "SIMPLE: Credential Name set correctly",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				CredentialName:    "some credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			valid: true,
		},
		{
			name: "SIMPLE CredentialName set with ClientCertificate specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				CredentialName:    "credential",
				ClientCertificate: "cert",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			valid: false,
		},
		{
			name: "SIMPLE: CredentialName set with PrivateKey specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "key",
				CaCertificates:    "",
			},
			valid: false,
		},
		{
			name: "SIMPLE: CredentialName set with CACertficiates specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "ca",
			},
			valid: false,
		},
		{
			name: "MUTUAL: Credential Name set correctly",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "some credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			valid: true,
		},
		{
			name: "MUTUAL CredentialName set with ClientCertificate specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "credential",
				ClientCertificate: "cert",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName set with PrivateKey specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "key",
				CaCertificates:    "",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName set with CACertficiates specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "ca",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName set with CACRL specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCrl:             "ca",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName not set with ClientCertificate and Key specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "cert",
				PrivateKey:        "key",
			},
			valid: true,
		},
		{
			name: "MUTUAL: CredentialName not set with ClientCertificate specified and Key missing",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "cert",
				PrivateKey:        "",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName not set with ClientCertificate missing and Key specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "",
				PrivateKey:        "key",
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		if got := ValidateTLS("", tc.tls); (got == nil) != tc.valid {
			t.Errorf("ValidateTLS(%q) => got valid=%v, want valid=%v",
				tc.name, got == nil, tc.valid)
		}
	}
}

func TestValidateWildcardDomain(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{"empty", "", "empty"},
		{"too long", strings.Repeat("x", 256), "too long"},
		{"happy", strings.Repeat("x", 63), ""},
		{"wildcard", "*", ""},
		{"wildcard multi-segment", "*.bar.com", ""},
		{"wildcard single segment", "*foo", ""},
		{"wildcard prefix", "*foo.bar.com", ""},
		{"wildcard prefix dash", "*-foo.bar.com", ""},
		{"bad wildcard", "foo.*.com", "invalid"},
		{"bad wildcard", "foo*.bar.com", "invalid"},
		{"IP address", "1.1.1.1", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWildcardDomain(tt.in)
			if err == nil && tt.out != "" {
				t.Fatalf("ValidateWildcardDomain(%v) = nil, wanted %q", tt.in, tt.out)
			} else if err != nil && tt.out == "" {
				t.Fatalf("ValidateWildcardDomain(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.out) {
				t.Fatalf("ValidateWildcardDomain(%v) = %v, wanted %q", tt.in, err, tt.out)
			}
		})
	}
}

func TestValidateWildcardDomainForVirtualServiceBoundToGateway(t *testing.T) {
	tests := []struct {
		name string
		in   string
		sni  bool
		out  string
	}{
		{"empty", "", false, "empty"},
		{"too long", strings.Repeat("x", 256), false, "too long"},
		{"happy", strings.Repeat("x", 63), false, ""},
		{"wildcard", "*", false, ""},
		{"wildcard multi-segment", "*.bar.com", false, ""},
		{"wildcard single segment", "*foo", false, ""},
		{"wildcard prefix", "*foo.bar.com", false, ""},
		{"wildcard prefix dash", "*-foo.bar.com", false, ""},
		{"bad wildcard", "foo.*.com", false, "invalid"},
		{"bad wildcard", "foo*.bar.com", false, "invalid"},
		{"IP address", "1.1.1.1", false, "invalid"},
		{"SNI domain", "outbound_.80_._.e2e.foobar.mesh", true, ""},
		{"happy", "outbound.com", true, ""},
		{"invalid SNI domain", "neverbound_.80_._.e2e.foobar.mesh", true, "invalid"},
		{"invalid SNI domain", "outbound_.thisIsNotAPort_._.e2e.foobar.mesh", true, "invalid"},
		{"invalid SNI domain", "outbound_.80_._", true, "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWildcardDomainForVirtualServiceBoundToGateway(tt.sni, tt.in)
			if err == nil && tt.out != "" {
				t.Fatalf("ValidateWildcardDomain(%v) = nil, wanted %q", tt.in, tt.out)
			} else if err != nil && tt.out == "" {
				t.Fatalf("ValidateWildcardDomain(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.out) {
				t.Fatalf("ValidateWildcardDomain(%v) = %v, wanted %q", tt.in, err, tt.out)
			}
		})
	}
}

func TestParseAutoGeneratedSNIDomain(t *testing.T) {
	tests := []struct {
		name                   string
		in                     string
		trafficDirectionSuffix string
		port                   string
		hostname               string
		err                    string
	}{
		{"invalid", "foobar.com", "", "", "", "domain name foobar.com invalid, should follow '" + outboundHostNameFormat + "' format"},
		{"valid", "outbound_.80_._.e2e.foobar.mesh", "outbound", "80", "e2e.foobar.mesh", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trafficDirectionSuffix, port, hostname, err := parseAutoGeneratedSNIDomain(tt.in)
			if err == nil && tt.err != "" {
				t.Fatalf("parseOutboundSNIDomain(%v) = nil, wanted %q", tt.in, tt.err)
			} else if err != nil && tt.err == "" {
				t.Fatalf("parseOutboundSNIDomain(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("parseOutboundSNIDomain(%v) = %v, wanted %q", tt.in, err, tt.err)
			}
			if trafficDirectionSuffix != tt.trafficDirectionSuffix {
				t.Fatalf("parseOutboundSNIDomain(%v) = %v, wanted %q", tt.in, trafficDirectionSuffix, tt.trafficDirectionSuffix)
			}
			if port != tt.port {
				t.Fatalf("parseOutboundSNIDomain(%v) = %v, wanted %q", tt.in, port, tt.port)
			}
			if hostname != tt.hostname {
				t.Fatalf("parseOutboundSNIDomain(%v) = %v, wanted %q", tt.in, hostname, tt.hostname)
			}
		})
	}
}

func TestValidateTrustDomain(t *testing.T) {
	tests := []struct {
		name string
		in   string
		err  string
	}{
		{"empty", "", "empty"},
		{"happy", strings.Repeat("x", 63), ""},
		{"multi-segment", "foo.bar.com", ""},
		{"middle dash", "f-oo.bar.com", ""},
		{"trailing dot", "foo.bar.com.", ""},
		{"prefix dash", "-foo.bar.com", "invalid"},
		{"forward slash separated", "foo/bar/com", "invalid"},
		{"colon separated", "foo:bar:com", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTrustDomain(tt.in)
			if err == nil && tt.err != "" {
				t.Fatalf("ValidateTrustDomain(%v) = nil, wanted %q", tt.in, tt.err)
			} else if err != nil && tt.err == "" {
				t.Fatalf("ValidateTrustDomain(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("ValidateTrustDomain(%v) = %v, wanted %q", tt.in, err, tt.err)
			}
		})
	}
}

func TestValidateConnectTimeout(t *testing.T) {
	type durationCheck struct {
		duration *durationpb.Duration
		isValid  bool
	}

	checks := []durationCheck{
		{
			duration: &durationpb.Duration{Seconds: 1},
			isValid:  true,
		},
		{
			duration: &durationpb.Duration{Seconds: 31},
			isValid:  true,
		},
		{
			duration: &durationpb.Duration{Seconds: 999999999},
			isValid:  false,
		},
	}

	for _, check := range checks {
		if got := ValidateConnectTimeout(check.duration); (got == nil) != check.isValid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, check.isValid, got, check.duration)
		}
	}
}

func TestValidateProtocolDetectionTimeout(t *testing.T) {
	type durationCheck struct {
		duration *durationpb.Duration
		isValid  bool
	}

	checks := []durationCheck{
		{
			duration: &durationpb.Duration{Seconds: 1},
			isValid:  true,
		},
		{
			duration: &durationpb.Duration{Nanos: 99999},
			isValid:  false,
		},
		{
			duration: &durationpb.Duration{Nanos: 0},
			isValid:  true,
		},
	}

	for _, check := range checks {
		if got := ValidateProtocolDetectionTimeout(check.duration); (got == nil) != check.isValid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, check.isValid, got, check.duration)
		}
	}
}

func TestValidateMeshConfig(t *testing.T) {
	if _, err := ValidateMeshConfig(&meshconfig.MeshConfig{}); err == nil {
		t.Error("expected an error on an empty mesh config")
	}

	invalid := &meshconfig.MeshConfig{
		ProxyListenPort:    0,
		ConnectTimeout:     durationpb.New(-1 * time.Second),
		DefaultConfig:      &meshconfig.ProxyConfig{},
		TrustDomain:        "",
		TrustDomainAliases: []string{"a.$b", "a/b", ""},
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "default",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp{
					EnvoyExtAuthzHttp: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationHttpProvider{
						Service: "foo/ext-authz",
						Port:    999999,
					},
				},
			},
		},
		MeshMTLS: &meshconfig.MeshConfig_TLSConfig{
			EcdhCurves: []string{"P-256"},
		},
		TlsDefaults: &meshconfig.MeshConfig_TLSConfig{
			EcdhCurves: []string{"P-256", "P-256", "invalid"},
		},
	}

	warning, err := ValidateMeshConfig(invalid)
	if err == nil {
		t.Errorf("expected an error on invalid proxy mesh config: %v", invalid)
	} else {
		wantErrors := []string{
			"invalid proxy listen port",
			"invalid connect timeout",
			"config path must be set",
			"binary path must be set",
			"oneof service cluster or tracing service name must be specified",
			"invalid drain duration: duration must be greater than 1ms",
			"discovery address must be set to the proxy discovery service",
			"invalid proxy admin port",
			"invalid status port",
			"trustDomain: empty domain name not allowed",
			"trustDomainAliases[0]",
			"trustDomainAliases[1]",
			"trustDomainAliases[2]",
			"mesh TLS does not support ECDH curves configuration",
		}
		switch err := err.(type) {
		case *multierror.Error:
			// each field must cause an error in the field
			if len(err.Errors) != len(wantErrors) {
				t.Errorf("expected %d errors but found %v", len(wantErrors), err)
			} else {
				for i := 0; i < len(wantErrors); i++ {
					if !strings.HasPrefix(err.Errors[i].Error(), wantErrors[i]) {
						t.Errorf("expected error %q at index %d but found %q", wantErrors[i], i, err.Errors[i])
					}
				}
			}
		default:
			t.Errorf("expected a multi error as output")
		}
	}
	if warning == nil {
		t.Errorf("expected a warning on invalid proxy mesh config: %v", invalid)
	} else {
		wantWarnings := []string{
			"detected unrecognized ECDH curves",
			"detected duplicate ECDH curves",
		}
		switch warn := warning.(type) {
		case *multierror.Error:
			// each field must cause an error in the field
			if len(warn.Errors) != len(wantWarnings) {
				t.Errorf("expected %d warnings but found %v", len(wantWarnings), warn)
			} else {
				for i := 0; i < len(wantWarnings); i++ {
					if !strings.HasPrefix(warn.Errors[i].Error(), wantWarnings[i]) {
						t.Errorf("expected warning %q at index %d but found %q", wantWarnings[i], i, warn.Errors[i])
					}
				}
			}
		default:
			t.Errorf("expected a multi error as output")
		}
	}
}

func TestValidateLocalityLbSetting(t *testing.T) {
	cases := []struct {
		name    string
		in      *networking.LocalityLoadBalancerSetting
		outlier *networking.OutlierDetection
		err     bool
		warn    bool
	}{
		{
			name:    "valid mesh config without LocalityLoadBalancerSetting",
			in:      nil,
			outlier: nil,
			err:     false,
			warn:    false,
		},

		{
			name: "invalid LocalityLoadBalancerSetting_Distribute total weight > 100",
			in: &networking.LocalityLoadBalancerSetting{
				Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 80,
							"a/b1":  25,
						},
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
		{
			name: "invalid LocalityLoadBalancerSetting_Distribute total weight < 100",
			in: &networking.LocalityLoadBalancerSetting{
				Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 80,
							"a/b1":  15,
						},
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
		{
			name: "invalid LocalityLoadBalancerSetting_Distribute weight = 0",
			in: &networking.LocalityLoadBalancerSetting{
				Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 0,
							"a/b1":  100,
						},
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
		{
			name: "invalid LocalityLoadBalancerSetting specify both distribute and failover",
			in: &networking.LocalityLoadBalancerSetting{
				Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 80,
							"a/b1":  20,
						},
					},
				},
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "region1",
						To:   "region2",
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},

		{
			name: "invalid failover src and dst have same region",
			in: &networking.LocalityLoadBalancerSetting{
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "region1",
						To:   "region1",
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
		{
			name: "invalid failover src contain '*' wildcard",
			in: &networking.LocalityLoadBalancerSetting{
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "*",
						To:   "region2",
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
		{
			name: "invalid failover dst contain '*' wildcard",
			in: &networking.LocalityLoadBalancerSetting{
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "region1",
						To:   "*",
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
		{
			name: "invalid failover src contain '/' separator",
			in: &networking.LocalityLoadBalancerSetting{
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "region1/zone1",
						To:   "region2",
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
		{
			name: "invalid failover dst contain '/' separator",
			in: &networking.LocalityLoadBalancerSetting{
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "region1",
						To:   "region2/zone1",
					},
				},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
		{
			name: "failover priority provided without outlier detection policy",
			in: &networking.LocalityLoadBalancerSetting{
				FailoverPriority: []string{
					"topology.istio.io/network",
					"topology.kubernetes.io/region",
					"topology.kubernetes.io/zone",
					"topology.istio.io/subzone",
				},
			},
			outlier: nil,
			err:     false,
			warn:    true,
		},
		{
			name: "failover provided without outlier detection policy",
			in: &networking.LocalityLoadBalancerSetting{
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "us-east",
						To:   "eu-west",
					},
					{
						From: "us-west",
						To:   "eu-east",
					},
				},
			},
			outlier: nil,
			err:     false,
			warn:    true,
		},
		{
			name: "invalid LocalityLoadBalancerSetting specify both failoverPriority and region in failover",
			in: &networking.LocalityLoadBalancerSetting{
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "region1",
						To:   "region2",
					},
				},
				FailoverPriority: []string{"topology.kubernetes.io/region"},
			},
			outlier: &networking.OutlierDetection{},
			err:     true,
			warn:    false,
		},
	}

	for _, c := range cases {
		v := ValidateLocalityLbSetting(c.in, c.outlier)
		warn, err := v.Unwrap()
		if (err != nil) != c.err {
			t.Errorf("ValidateLocalityLbSetting failed on %v: got err=%v but wanted err=%v: %v",
				c.name, err != nil, c.err, err)
		}
		if (warn != nil) != c.warn {
			t.Errorf("ValidateLocalityLbSetting failed on %v: got warn=%v but wanted warn=%v: %v",
				c.name, warn != nil, c.warn, warn)
		}
	}
}

func TestValidateLocalities(t *testing.T) {
	cases := []struct {
		name       string
		localities []string
		valid      bool
	}{
		{
			name:       "multi wildcard locality",
			localities: []string{"*/zone/*"},
			valid:      false,
		},
		{
			name:       "wildcard not in suffix",
			localities: []string{"*/zone"},
			valid:      false,
		},
		{
			name:       "explicit wildcard region overlap",
			localities: []string{"*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard region overlap",
			localities: []string{"a", "a/b/c"},
			valid:      false,
		},
		{
			name:       "explicit wildcard zone overlap",
			localities: []string{"a/*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard zone overlap",
			localities: []string{"a/b", "a/b/c"},
			valid:      false,
		},
		{
			name:       "explicit wildcard subzone overlap",
			localities: []string{"a/b/*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard subzone overlap",
			localities: []string{"a/b", "a/b/c"},
			valid:      false,
		},
		{
			name:       "valid localities",
			localities: []string{"a1/*", "a2/*", "a3/b3/c3", "a4/b4", "a5/b5/*"},
			valid:      true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateLocalities(c.localities)
			if !c.valid && err == nil {
				t.Errorf("expect invalid localities")
			}

			if c.valid && err != nil {
				t.Errorf("expect valid localities. but got err %v", err)
			}
		})
	}
}

func TestValidationIPAddress(t *testing.T) {
	tests := []struct {
		name string
		addr string
		ok   bool
	}{
		{
			name: "valid ipv4 address",
			addr: "1.1.1.1",
			ok:   true,
		},
		{
			name: "invalid ipv4 address",
			addr: "1.1.A.1",
			ok:   false,
		},
		{
			name: "valid ipv6 subnet",
			addr: "2001:1::1",
			ok:   true,
		},
		{
			name: "invalid ipv6 address",
			addr: "2001:1:G::1",
			ok:   false,
		},
	}
	for _, tt := range tests {
		if err := ValidateIPAddress(tt.addr); err != nil {
			if tt.ok {
				t.Errorf("test: \"%s\" expected to succeed but failed with error: %+v", tt.name, err)
			}
		} else {
			if !tt.ok {
				t.Errorf("test: \"%s\" expected to fail but succeeded", tt.name)
			}
		}
	}
}

func TestValidationIPSubnet(t *testing.T) {
	tests := []struct {
		name   string
		subnet string
		ok     bool
	}{
		{
			name:   "valid ipv4 subnet",
			subnet: "1.1.1.1/24",
			ok:     true,
		},
		{
			name:   "invalid ipv4 subnet",
			subnet: "1.1.1.1/48",
			ok:     false,
		},
		{
			name:   "valid ipv6 subnet",
			subnet: "2001:1::1/64",
			ok:     true,
		},
		{
			name:   "invalid ipv6 subnet",
			subnet: "2001:1::1/132",
			ok:     false,
		},
	}

	for _, tt := range tests {
		if err := ValidateIPSubnet(tt.subnet); err != nil {
			if tt.ok {
				t.Errorf("test: \"%s\" expected to succeed but failed with error: %+v", tt.name, err)
			}
		} else {
			if !tt.ok {
				t.Errorf("test: \"%s\" expected to fail but succeeded", tt.name)
			}
		}
	}
}

func TestServiceSettings(t *testing.T) {
	cases := []struct {
		name  string
		hosts []string
		valid bool
	}{
		{
			name: "good",
			hosts: []string{
				"*.foo.bar",
				"bar.baz.svc.cluster.local",
			},
			valid: true,
		},
		{
			name: "bad",
			hosts: []string{
				"foo.bar.*",
			},
			valid: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := meshconfig.MeshConfig{
				ServiceSettings: []*meshconfig.MeshConfig_ServiceSettings{
					{
						Hosts: c.hosts,
					},
				},
			}

			if got := validateServiceSettings(&m); (got == nil) != c.valid {
				t.Errorf("got(%v) != want(%v)\n", got, c.valid)
			}
		})
	}
}

func TestValidateMeshNetworks(t *testing.T) {
	testcases := []struct {
		name  string
		mn    *meshconfig.MeshNetworks
		valid bool
	}{
		{
			name:  "Empty MeshNetworks",
			mn:    &meshconfig.MeshNetworks{},
			valid: true,
		},
		{
			name: "Valid MeshNetworks",
			mn: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"n1": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
									FromRegistry: "Kubernetes",
								},
							},
						},
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
									RegistryServiceName: "istio-ingressgateway.istio-system.svc.cluster.local",
								},
								Port: 80,
							},
						},
					},
					"n2": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
									FromRegistry: "cluster1",
								},
							},
						},
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
									RegistryServiceName: "istio-ingressgateway.istio-system.svc.cluster.local",
								},
								Port: 443,
							},
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "Invalid Gateway Address",
			mn: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"n1": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
									FromRegistry: "Kubernetes",
								},
							},
						},
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_Address{
									Address: "1nv@lidhostname",
								},
								Port: 80,
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Invalid registry name",
			mn: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"n1": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
									FromRegistry: "cluster.local",
								},
							},
						},
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
									RegistryServiceName: "istio-ingressgateway.istio-system.svc.cluster.local",
								},
								Port: 80,
							},
						},
					},
				},
			},
			valid: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMeshNetworks(tc.mn)
			if err != nil && tc.valid {
				t.Errorf("error not expected on valid meshnetworks: %v", tc.mn)
			}
			if err == nil && !tc.valid {
				t.Errorf("expected an error on invalid meshnetworks: %v", tc.mn)
			}
		})
	}
}
