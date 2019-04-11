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

package env

import (
	gpb "github.com/gogo/protobuf/types"

	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
)

var (
	meshIP1 = []byte{1, 1, 1, 1}
	meshIP2 = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 204, 152, 189, 116}
)

// MixerFilterConf stores config for Mixer filter.
type MixerFilterConf struct {
	PerRouteConf   *mccpb.ServiceConfig
	HTTPServerConf *mccpb.HttpClientConfig
	HTTPClientConf *mccpb.HttpClientConfig
	TCPServerConf  *mccpb.TcpClientConfig
}

// GetDefaultMixerFilterConf get config for Mixer filter
func GetDefaultMixerFilterConf() *MixerFilterConf {
	return &MixerFilterConf{
		PerRouteConf:   GetDefaultServiceConfig(),
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
				"mesh2.ip":    {Value: &mpb.Attributes_AttributeValue_BytesValue{BytesValue: meshIP2}},
				"target.user": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "target-user"}},
				"target.name": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "target-name"}},
			},
		},
	}
}

// GetDefaultHTTPServerConf get default HTTP server config
func GetDefaultHTTPServerConf() *mccpb.HttpClientConfig {
	mfConf := &mccpb.HttpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"mesh1.ip":         {Value: &mpb.Attributes_AttributeValue_BytesValue{BytesValue: meshIP1}},
				"target.uid":       {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "POD222"}},
				"target.namespace": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "XYZ222"}},
			},
		},
	}
	return mfConf
}

// GetDefaultHTTPClientConf get default HTTP client config
func GetDefaultHTTPClientConf() *mccpb.HttpClientConfig {
	mfConf := &mccpb.HttpClientConfig{
		ForwardAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"source.uid":       {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "POD11"}},
				"source.namespace": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "XYZ11"}},
			},
		},
	}
	return mfConf
}

// GetDefaultTCPServerConf get default TCP server config
func GetDefaultTCPServerConf() *mccpb.TcpClientConfig {
	mfConf := &mccpb.TcpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"mesh1.ip":         {Value: &mpb.Attributes_AttributeValue_BytesValue{BytesValue: meshIP1}},
				"target.uid":       {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "POD222"}},
				"target.namespace": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "XYZ222"}},
			},
		},
	}
	return mfConf
}

// SetNetworPolicy set network policy
func SetNetworPolicy(mfConf *mccpb.HttpClientConfig, open bool) {
	if mfConf.Transport == nil {
		mfConf.Transport = &mccpb.TransportConfig{}
	}
	mfConf.Transport.NetworkFailPolicy = &mccpb.NetworkFailPolicy{}
	if open {
		mfConf.Transport.NetworkFailPolicy.Policy = mccpb.FAIL_OPEN
	} else {
		mfConf.Transport.NetworkFailPolicy.Policy = mccpb.FAIL_CLOSE
	}
}

// DisableHTTPClientCache disable HTTP client cache
func DisableHTTPClientCache(mfConf *mccpb.HttpClientConfig, checkCache, quotaCache, reportBatch bool) {
	if mfConf.Transport == nil {
		mfConf.Transport = &mccpb.TransportConfig{}
	}
	mfConf.Transport.DisableCheckCache = checkCache
	mfConf.Transport.DisableQuotaCache = quotaCache
	mfConf.Transport.DisableReportBatch = reportBatch
}

// DisableTCPClientCache disable TCP client cache
func DisableTCPClientCache(mfConf *mccpb.TcpClientConfig, checkCache, quotaCache, reportBatch bool) {
	if mfConf.Transport == nil {
		mfConf.Transport = &mccpb.TransportConfig{}
	}
	mfConf.Transport.DisableCheckCache = checkCache
	mfConf.Transport.DisableQuotaCache = quotaCache
	mfConf.Transport.DisableReportBatch = reportBatch
}

// DisableHTTPCheckReport disable HTTP check report
func DisableHTTPCheckReport(mfConf *MixerFilterConf, disableCheck, disableReport bool) {
	mfConf.PerRouteConf.DisableCheckCalls = disableCheck
	mfConf.PerRouteConf.DisableReportCalls = disableReport
}

// AddHTTPQuota add HTTP quota config
func AddHTTPQuota(mfConf *MixerFilterConf, quota string, charge int64) {
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

	mfConf.PerRouteConf.QuotaSpec = make([]*mccpb.QuotaSpec, 1)
	mfConf.PerRouteConf.QuotaSpec[0] = q
}

// DisableTCPCheckReport disable TCP check report.
func DisableTCPCheckReport(mfConf *mccpb.TcpClientConfig, disableCheck, disableReport bool) {
	mfConf.DisableCheckCalls = disableCheck
	mfConf.DisableReportCalls = disableReport
}

// SetTCPReportInterval sets TCP filter report interval in seconds
func SetTCPReportInterval(mfConf *mccpb.TcpClientConfig, reportInterval int64) {
	if mfConf.ReportInterval == nil {
		mfConf.ReportInterval = &gpb.Duration{
			Seconds: reportInterval,
		}
	} else {
		mfConf.ReportInterval.Seconds = reportInterval
	}
}

// SetStatsUpdateInterval sets stats update interval for Mixer client filters in seconds.
func SetStatsUpdateInterval(mfConf *MixerFilterConf, updateInterval int64) {
	if mfConf.HTTPServerConf.Transport == nil {
		mfConf.HTTPServerConf.Transport = &mccpb.TransportConfig{}
	}
	mfConf.HTTPServerConf.Transport.StatsUpdateInterval = &gpb.Duration{
		Seconds: updateInterval,
	}
	if mfConf.TCPServerConf.Transport == nil {
		mfConf.TCPServerConf.Transport = &mccpb.TransportConfig{}
	}
	mfConf.TCPServerConf.Transport.StatsUpdateInterval = &gpb.Duration{
		Seconds: updateInterval,
	}
}

// SetDefaultServiceConfigMap set the default service config to the service config map
func SetDefaultServiceConfigMap(mfConf *MixerFilterConf) {
	service := ":default"
	mfConf.HTTPServerConf.DefaultDestinationService = service

	mfConf.HTTPServerConf.ServiceConfigs = map[string]*mccpb.ServiceConfig{}
	mfConf.HTTPServerConf.ServiceConfigs[service] = mfConf.PerRouteConf
}
