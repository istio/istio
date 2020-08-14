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

package util

import (
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	v1alpha12 "istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/env"
)

func TestOverlayIOP(t *testing.T) {
	defaultFilepath := filepath.Join(env.IstioSrc, "manifests/profiles/default.yaml")
	b, err := ioutil.ReadFile(defaultFilepath)
	if err != nil {
		t.Fatal(err)
	}
	// overlaying tree over itself exercises all paths for merging
	if _, err := OverlayIOP(string(b), string(b)); err != nil {
		t.Fatal(err)
	}
}

func TestOverlayIOPDefaultMeshConfig(t *testing.T) {
	// Transform default mesh config into map[string]interface{} for inclusion in IstioOperator.
	mcfg := meshconfig.MeshConfig{
		EnableTracing:               true,
		AccessLogFile:               "",
		AccessLogEncoding:           meshconfig.MeshConfig_TEXT,
		AccessLogFormat:             "",
		EnableEnvoyAccessLogService: false,
		ProtocolDetectionTimeout:    types.DurationProto(5 * time.Second),
		IngressService:              "istio-ingressgateway",
		IngressControllerMode:       meshconfig.MeshConfig_STRICT,
		IngressClass:                "istio",
		TrustDomain:                 "cluster.local",
		TrustDomainAliases:          []string{},
		EnableAutoMtls:              &types.BoolValue{Value: true},
		OutboundTrafficPolicy:       &meshconfig.MeshConfig_OutboundTrafficPolicy{Mode: meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY},
		LocalityLbSetting: &v1alpha3.LocalityLoadBalancerSetting{
			Enabled: &types.BoolValue{Value: true},
		},
		Certificates: []*meshconfig.Certificate{},

		// Not set in the default mesh config - code defaults.
		RootNamespace:                  constants.IstioSystemNamespace,
		ProxyListenPort:                15001,
		ConnectTimeout:                 types.DurationProto(10 * time.Second),
		EnableSdsTokenMount:            false,
		DefaultServiceExportTo:         []string{"*"},
		DefaultVirtualServiceExportTo:  []string{"*"},
		DefaultDestinationRuleExportTo: []string{"*"},
		DnsRefreshRate:                 types.DurationProto(5 * time.Second), // 5 seconds is the default refresh rate used in Envoy
		ThriftConfig:                   &meshconfig.MeshConfig_ThriftConfig{},
		ServiceSettings:                make([]*meshconfig.MeshConfig_ServiceSettings, 0),
	}
	my, err := yaml.Marshal(mcfg)
	if err != nil {
		t.Fatal(err)
	}
	mm := make(map[string]interface{})
	if err := yaml.Unmarshal(my, &mm); err != nil {
		t.Fatal(err)
	}
	iop := &v1alpha1.IstioOperator{
		Spec: &v1alpha12.IstioOperatorSpec{
			MeshConfig: mm,
		},
	}

	iy, err := yaml.Marshal(iop)
	if err != nil {
		t.Fatal(err)
	}

	// overlaying tree over itself exercises all paths for merging
	if _, err := OverlayIOP(string(iy), string(iy)); err != nil {
		t.Fatal(err)
	}
}
