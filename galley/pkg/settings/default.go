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
	"istio.io/istio/pkg/ctrlz"
)

func Default() *Galley {
	cfg := &Galley{}
	cfg.General = defaultGeneral()
	cfg.Validation = defaultValidation()
	cfg.Processing = defaultProcessing()
	return cfg
}

func defaultGeneral() *General {
	return &General{
		MonitoringPort:  9093,
		PprofPort:       9094,
		EnableProfiling: false,
		KubeConfig:      "",
		MeshConfigFile:  "/etc/mesh-config/mesh",
		Liveness:        defaultLiveness(),
		Readiness:       defaultReadiness(),
		Introspection:   defaultIntrospection(),
	}
}

func defaultProcessing() *Processing {
	return &Processing{
		Source: &Source{
			Source: &Source_Kubernetes{
				Kubernetes: &KubernetesSource{
					ResyncPeriod: ptypes.DurationProto(0),
				},
			},
		},
		Server: &Server{
			Address:                "tcp://0.0.0.0:9901",
			MaxReceivedMessageSize: 1024 * 1024,
			MaxConcurrentStreams:   1024,
			Insecure:               false,
		},
		DomainSuffix: "cluster.local",
	}
}

func defaultValidation() *Validation {
	return &Validation{
		WebhookConfigFile:   "etc/config/validatingwebhookconfiguration.yaml",
		WebhookPort:         443,
		WebhookName:         "istio-galley",
		DeploymentNamespace: "istio-system",
		DeploymentName:      "istio-galley",
		ServiceName:         "istio-galley",
		Tls: &TLS{
			PrivateKey:        "/etc/certs/key.pem",
			ClientCertificate: "/etc/certs/cert-chain.pem",
			CaCertificates:    "/etc/certs/cert-chain.pem",
		},
	}
}

func defaultIntrospection() *Introspection {
	a := ctrlz.DefaultOptions()
	return &Introspection{
		Port:    int32(a.Port),
		Address: a.Address,
	}
}

func defaultLiveness() *Probe {
	return &Probe{
		Path:     "/healthLiveness",
		Interval: ptypes.DurationProto(2 * time.Second),
	}
}

func defaultReadiness() *Probe {
	return &Probe{
		Path:     "/healthReadiness",
		Interval: ptypes.DurationProto(2 * time.Second),
	}
}
