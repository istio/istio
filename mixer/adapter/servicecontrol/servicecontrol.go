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

//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -a mixer/adapter/servicecontrol/config/config.proto -x "-n servicecontrol -t apikey -t servicecontrolreport -t quota"
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -t mixer/adapter/servicecontrol/template/servicecontrolreport/template.proto

package servicecontrol

import (
	"context"
	"errors"
	"fmt"

	pbtypes "github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/adapter/servicecontrol/template/servicecontrolreport"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/apikey"
	"istio.io/istio/mixer/template/quota"
	"istio.io/istio/pkg/cache"
)

// servicecontrol adapter builder
type builder struct {
	config          *config.Params // Handler config
	checkDataShape  map[string]*apikey.Type
	reportDataShape map[string]*servicecontrolreport.Type
	quotaDataShape  map[string]*quota.Type
}

////// Builder method from supported template //////

// SetApiKeyTypes sets apiKey template data type.
func (b *builder) SetApiKeyTypes(types map[string]*apikey.Type) {
	b.checkDataShape = types
}

// SetServicecontrolReportTypes sets servicecontrolreport template data type.
func (b *builder) SetServicecontrolReportTypes(types map[string]*servicecontrolreport.Type) {
	b.reportDataShape = types
}

// SetQuotaTypes sets qutoa template data type.
func (b *builder) SetQuotaTypes(types map[string]*quota.Type) {
	b.quotaDataShape = types
}

////// adapter.HandlerBuilder interface //////

// SetAdapterConfig sets adapter config on builder.
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.config = cfg.(*config.Params)
	if b.config == nil {
		panic("fail to convert to config proto")
	}
}

// Validate validates adapter config.
func (b *builder) Validate() *adapter.ConfigErrors {
	result := validateRuntimeConfig(b.config.RuntimeConfig)
	result = multierror.Append(result, validateGcpServiceSetting(b.config.ServiceConfigs))
	if result.ErrorOrNil() != nil {
		return &adapter.ConfigErrors{Multi: result}
	}
	return nil
}

func validateRuntimeConfig(config *config.RuntimeConfig) *multierror.Error {
	var result *multierror.Error
	if config == nil {
		result = multierror.Append(result, errors.New("config is nil"))
		return result
	}

	if config.CheckResultExpiration == nil {
		result = multierror.Append(result, errors.New("config.CheckResultExpiration is nil"))
		return result
	}
	exp, err := pbtypes.DurationFromProto(config.CheckResultExpiration)
	if err != nil {
		result = multierror.Append(result, err)
	} else if exp <= 0 {
		result = multierror.Append(
			result, fmt.Errorf("expect positive CheckResultExpiration, but get %v", exp))
	}

	return result
}

func validateGcpServiceSetting(settings []*config.GcpServiceSetting) *multierror.Error {
	var result *multierror.Error
	if len(settings) == 0 {
		result = multierror.Append(result, errors.New("settings is nil or empty"))
		return result
	}
	for _, setting := range settings {
		if setting.MeshServiceName == "" || setting.GoogleServiceName == "" {
			result = multierror.Append(result,
				errors.New("settings.MeshServiceName and settings.GoogleServiceName must be non-empty"))
		}

		if setting.Quotas != nil {
			for _, qCfg := range setting.Quotas {
				if qCfg.Name == "" {
					result = multierror.Append(result, errors.New("encountered an empty QuotaName"))
				}
				if qCfg.Expiration == nil {
					result = multierror.Append(result, errors.New("quota expiration is nil"))
				} else {
					expiration, err := pbtypes.DurationFromProto(qCfg.Expiration)
					if err != nil {
						result = multierror.Append(result, err)
					} else if expiration <= 0 {
						result = multierror.Append(
							result, fmt.Errorf(
								`quota must have positive expiration, but get %v`, expiration))
					}
				}
			}
		}
	}
	return result
}

// Build builds an adapter handler.
func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	var _ apikey.HandlerBuilder = (*builder)(nil)
	var _ servicecontrolreport.HandlerBuilder = (*builder)(nil)
	var _ quota.HandlerBuilder = (*builder)(nil)

	client, err := newClient(b.config.CredentialPath)
	if err != nil {
		return nil, err
	}

	ctx, err := initializeHandlerContext(env, b.config, client)
	if err != nil {
		return nil, err
	}
	ctx.checkDataShape = b.checkDataShape
	ctx.reportDataShape = b.reportDataShape
	return newHandler(ctx)
}

func initializeHandlerContext(env adapter.Env, adapterCfg *config.Params,
	client serviceControlClient) (*handlerContext, error) {

	configIndex := make(map[string]*config.GcpServiceSetting, len(adapterCfg.ServiceConfigs))
	for _, cfg := range adapterCfg.ServiceConfigs {
		configIndex[cfg.MeshServiceName] = cfg
	}

	cacheExp := toDuration(adapterCfg.RuntimeConfig.CheckResultExpiration)
	// Set eviction interval to half of expiration time. That said the cache would scan and evict expired entries every
	// half of expiation time period.
	checkCache := cache.NewLRU(cacheExp, cacheExp/2, adapterCfg.RuntimeConfig.CheckCacheSize)

	return &handlerContext{
		env:                env,
		config:             adapterCfg,
		serviceConfigIndex: configIndex,
		checkResponseCache: checkCache,
		client:             client,
	}, nil
}

// GetInfo registers Adapter with Mixer.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "servicecontrol",
		Impl:        "istio.io/istio/mixer/adapter/servicecontrol",
		Description: "Interface to Google Service Control",
		SupportedTemplates: []string{
			apikey.TemplateName,
			servicecontrolreport.TemplateName,
			quota.TemplateName,
		},
		DefaultConfig: &config.Params{},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
	}
}
