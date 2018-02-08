// Copyright 2017 Istio Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package env

import (
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
)

var (
	meshIP1 = []byte{1, 1, 1, 1}
	meshIP2 = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 204, 152, 189, 116}
	meshIP3 = []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 8}
)

// V2Conf stores v2 config.
type V2Conf struct {
	HTTPServerConf *mccpb.HttpClientConfig
	HTTPClientConf *mccpb.HttpClientConfig
	TCPServerConf  *mccpb.TcpClientConfig
}

// GetDefaultV2Conf get V2 config
func GetDefaultV2Conf() *V2Conf {
	return &V2Conf{
		HTTPServerConf: GetDefaultHTTPServerConf(),
		HTTPClientConf: GetDefaultHTTPClientConf(),
		TCPServerConf:  GetDefaultTCPServerConf(),
	}
}

// GetDefaultServiceConfig get default service config
func GetDefaultServiceConfig() *mccpb.ServiceConfig {
     return &mccpb.ServiceConfig{
	  MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"mesh2.ip":    {Value: &mpb.Attributes_AttributeValue_BytesValue{meshIP2}},
				"target.user": {Value: &mpb.Attributes_AttributeValue_StringValue{"target-user"}},
				"target.name": {Value: &mpb.Attributes_AttributeValue_StringValue{"target-name"}},
			},
		},
		// TODO per-service HTTPApiApsec, QuotaSpec
	}
}

// GetDefaultHTTPServerConf get default HTTP server config
func GetDefaultHTTPServerConf() *mccpb.HttpClientConfig {
	v2 := &mccpb.HttpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"mesh1.ip":         {Value: &mpb.Attributes_AttributeValue_BytesValue{meshIP1}},
				"target.uid":       {Value: &mpb.Attributes_AttributeValue_StringValue{"POD222"}},
				"target.namespace": {Value: &mpb.Attributes_AttributeValue_StringValue{"XYZ222"}},
			},
		},
		ServiceConfigs: map[string]*mccpb.ServiceConfig{},
	}
	service := ":default"
	v2.DefaultDestinationService = service
	v2.ServiceConfigs[service] = GetDefaultServiceConfig()
	return v2
}

// GetDefaultHTTPClientConf get default HTTP client config
func GetDefaultHTTPClientConf() *mccpb.HttpClientConfig {
	v2 := &mccpb.HttpClientConfig{
		ForwardAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"mesh3.ip":         {Value: &mpb.Attributes_AttributeValue_BytesValue{meshIP3}},
				"source.uid":       {Value: &mpb.Attributes_AttributeValue_StringValue{"POD11"}},
				"source.namespace": {Value: &mpb.Attributes_AttributeValue_StringValue{"XYZ11"}},
			},
		},
		ServiceConfigs: map[string]*mccpb.ServiceConfig{},
	}
	return v2
}

// GetDefaultTCPServerConf get default TCP server config
func GetDefaultTCPServerConf() *mccpb.TcpClientConfig {
	v2 := &mccpb.TcpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"mesh1.ip":         {Value: &mpb.Attributes_AttributeValue_BytesValue{meshIP1}},
				"target.uid":       {Value: &mpb.Attributes_AttributeValue_StringValue{"POD222"}},
				"target.namespace": {Value: &mpb.Attributes_AttributeValue_StringValue{"XYZ222"}},
			},
		},
	}
	return v2
}

// SetNetworPolicy set network policy
func SetNetworPolicy(v2 *mccpb.HttpClientConfig, open bool) {
	if v2.Transport == nil {
		v2.Transport = &mccpb.TransportConfig{}
	}
	if open {
		v2.Transport.NetworkFailPolicy = mccpb.FAIL_OPEN
	} else {
		v2.Transport.NetworkFailPolicy = mccpb.FAIL_CLOSE
	}
}

// DisableClientCache disable client cache
func DisableClientCache(v2 *mccpb.HttpClientConfig, checkCache, quotaCache, reportBatch bool) {
	if v2.Transport == nil {
		v2.Transport = &mccpb.TransportConfig{}
	}
	v2.Transport.DisableCheckCache = checkCache
	v2.Transport.DisableQuotaCache = quotaCache
	v2.Transport.DisableReportBatch = reportBatch
}

// DisableHTTPCheckReport disable HTTP check report
func DisableHTTPCheckReport(v2 *mccpb.HttpClientConfig, disableCheck, disableReport bool) {
	for _, s := range v2.ServiceConfigs {
		s.DisableCheckCalls = disableCheck
		s.DisableReportCalls = disableReport
	}
}

// AddHTTPQuota add HTTP quota config
func AddHTTPQuota(v2 *mccpb.HttpClientConfig, quota string, charge int64) {
	q := &mccpb.QuotaSpec{
		Rules: make([]*mccpb.QuotaRule, 1),
	}
	q.Rules[0] = &mccpb.QuotaRule{
		Quotas: make([]*mccpb.Quota, 1),
	}
	q.Rules[0].Quotas[0] = &mccpb.Quota{
		Quota:  quota,
		Charge: charge,
	}

	for _, s := range v2.ServiceConfigs {
		s.QuotaSpec = make([]*mccpb.QuotaSpec, 1)
		s.QuotaSpec[0] = q
	}
}

// DisableTCPCheckReport disable TCP check report.
func DisableTCPCheckReport(v2 *mccpb.TcpClientConfig, disableCheck, disableReport bool) {
	v2.DisableCheckCalls = disableCheck
	v2.DisableReportCalls = disableReport
}

// AddJwtAuth add JWT auth.
func AddJwtAuth(v2 *mccpb.HttpClientConfig, jwt *mccpb.JWT) {
	for _, s := range v2.ServiceConfigs {
		if s.EndUserAuthnSpec == nil {
			s.EndUserAuthnSpec = &mccpb.EndUserAuthenticationPolicySpec{}
		}
		s.EndUserAuthnSpec.Jwts = append(s.EndUserAuthnSpec.Jwts, jwt)
	}
}
