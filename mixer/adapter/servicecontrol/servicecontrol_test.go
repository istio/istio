// Copyright 2017 Istio Authors
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

package servicecontrol

import (
	"reflect"
	"testing"

	pbtypes "github.com/gogo/protobuf/types"

	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/adapter/servicecontrol/template/servicecontrolreport"
	at "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/apikey"
	"istio.io/istio/mixer/template/quota"
)

func TestInitializeHandlerContext(t *testing.T) {
	adapterCfg := getTestAdapterConfig()
	ctx, err := initializeHandlerContext(at.NewEnv(t), adapterCfg, &mockSvcctrlClient{})
	if err != nil {
		t.Errorf("initializeHandlerContext() failed with %v", err)
	}

	expectedIdx := map[string]*config.GcpServiceSetting{
		"service_a": adapterCfg.ServiceConfigs[0],
		"service_b": adapterCfg.ServiceConfigs[1],
	}
	if !reflect.DeepEqual(expectedIdx, ctx.serviceConfigIndex) {
		t.Errorf("expect serviceConfigIndex :%v, but get %v",
			expectedIdx, ctx.serviceConfigIndex)
	}
	if ctx.checkResponseCache == nil {
		t.Errorf("fail to initialize check cache")
	}
}

func TestConfigValidation(t *testing.T) {

	{
		b := getTestBuilder()
		err := b.Validate()
		if err != nil {
			t.Errorf(`valid config, but get error %v`, err.Multi)
		}
	}

	invalidBuilders := []*builder{
		func() *builder {
			b := getTestBuilder()
			b.config.RuntimeConfig = nil
			return b
		}(),
		func() *builder {
			b := getTestBuilder()
			b.config.ServiceConfigs = []*config.GcpServiceSetting{}
			return b
		}(),
		func() *builder {
			b := getTestBuilder()
			b.config.ServiceConfigs = nil
			return b
		}(),
		func() *builder {
			b := getTestBuilder()
			b.config.ServiceConfigs[0].MeshServiceName = ""
			return b
		}(),
		func() *builder {
			b := getTestBuilder()
			b.config.ServiceConfigs[0].GoogleServiceName = ""
			return b
		}(),
		func() *builder {
			b := getTestBuilder()
			b.config.ServiceConfigs[0].Quotas[0].Name = ""
			return b
		}(),
		func() *builder {
			b := getTestBuilder()
			b.config.ServiceConfigs[0].Quotas[0].Expiration = nil
			return b
		}(),
		func() *builder {
			b := getTestBuilder()
			expiration := b.config.ServiceConfigs[0].Quotas[0].Expiration
			expiration.Seconds = 0
			expiration.Nanos = 0
			return b
		}(),
	}

	for _, b := range invalidBuilders {
		err := b.Validate()
		if err == nil {
			t.Errorf(`fail to detect invalid config: %v`, *b.config)
		}
	}
}

func TestGetInfo(t *testing.T) {
	info := GetInfo()
	expectedSupportedTemplate := []string{
		apikey.TemplateName,
		servicecontrolreport.TemplateName,
		quota.TemplateName,
	}
	if !reflect.DeepEqual(expectedSupportedTemplate, info.SupportedTemplates) {
		t.Errorf("expected supported templates: %v, but get %v",
			expectedSupportedTemplate,
			info.SupportedTemplates)
	}
}

func getTestAdapterConfig() *config.Params {
	return &config.Params{
		RuntimeConfig: &config.RuntimeConfig{CheckCacheSize: 10,
			CheckResultExpiration: &pbtypes.Duration{
				Seconds: 10,
			},
		},
		ServiceConfigs: []*config.GcpServiceSetting{
			{
				MeshServiceName:   "service_a",
				GoogleServiceName: "service_a.googleapi.com",
				Quotas: []*config.Quota{
					{
						Name:                  "request-count",
						GoogleQuotaMetricName: "request-metric",
						Expiration: &pbtypes.Duration{
							Seconds: 10,
						},
					},
				},
			},
			{
				MeshServiceName:   "service_b",
				GoogleServiceName: "service_b.googleapi.com",
			},
		},
	}
}

func getTestBuilder() *builder {
	return &builder{
		config: getTestAdapterConfig(),
	}
}
