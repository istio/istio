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
	"time"

	"github.com/golang/protobuf/ptypes"
	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/mcp/creds"
)

// Default returns a default configuration.
func Default() *Galley {
	cfg := &Galley{}
	cfg.General = defaultGeneral()
	cfg.Validation = defaultValidation()
	cfg.Processing = defaultProcessing()
	return cfg
}

func defaultGeneral() General {
	s := server.DefaultArgs()

	return General{
		MonitoringPort:  9093,
		PprofPort:       9094,
		EnableProfiling: false,
		KubeConfig:      "",
		MeshConfigFile:  s.MeshConfigFile,
		Liveness:        defaultLiveness(),
		Readiness:       defaultReadiness(),
		Introspection:   defaultIntrospection(),
	}
}

func defaultProcessing() Processing {
	s := server.DefaultArgs()

	var provider AuthProvider
	if s.Insecure {
		provider = AuthProvider{
			Provider: &AuthProvider_Insecure{
				Insecure: &InsecureAuthProvider{},
			},
		}
	} else {
		c := creds.DefaultOptions()
		provider = AuthProvider{
			Provider: &AuthProvider_Mtls{
				Mtls: &MTLSAuthProvider{
					ClientCertificate: c.CertificateFile,
					PrivateKey:        c.KeyFile,
					CaCertificates:    c.CACertificateFile,
					AccessListFile:    s.AccessListFile,
				},
			},
		}
	}

	return Processing{
		Source: Source{
			Source: &Source_Kubernetes{
				Kubernetes: &KubernetesSource{
					ResyncPeriod: ptypes.DurationProto(0),
				},
			},
		},

		Server: Server{
			Disable:                   false,
			DisableResourceReadyCheck: s.DisableResourceReadyCheck,
			GrpcTracing:               s.EnableGRPCTracing,
			Address:                   s.APIAddress,
			MaxReceivedMessageSize:    uint32(s.MaxReceivedMessageSize),
			MaxConcurrentStreams:      uint32(s.MaxConcurrentStreams),
			Auth:                      provider,
		},
		DomainSuffix: s.DomainSuffix,
	}
}

func defaultValidation() Validation {
	v := validation.DefaultArgs()
	return Validation{
		Disable:             !v.EnableValidation,
		WebhookConfigFile:   "/etc/config/validatingwebhookconfiguration.yaml",
		WebhookPort:         uint32(v.Port),
		WebhookName:         v.WebhookName,
		DeploymentNamespace: v.DeploymentAndServiceNamespace,
		DeploymentName:      v.DeploymentName,
		ServiceName:         v.ServiceName,
		Tls: TLS{
			PrivateKey:        v.KeyFile,
			ClientCertificate: v.CertFile,
			CaCertificates:    v.CACertFile,
		},
	}
}

func defaultIntrospection() Introspection {
	a := ctrlz.DefaultOptions()
	return Introspection{
		Port:    uint32(a.Port),
		Address: a.Address,
	}
}

func defaultLiveness() Probe {
	return Probe{
		Path:     "/healthLiveness",
		Interval: ptypes.DurationProto(1 * time.Second),
	}
}

func defaultReadiness() Probe {
	return Probe{
		Path:     "/healthReadiness",
		Interval: ptypes.DurationProto(1 * time.Second),
	}
}
