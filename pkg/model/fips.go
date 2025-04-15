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

package model

import (
	gotls "crypto/tls"
	"errors"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	common_features "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
)

var fipsCiphers = []string{
	"ECDHE-ECDSA-AES128-GCM-SHA256",
	"ECDHE-RSA-AES128-GCM-SHA256",
	"ECDHE-ECDSA-AES256-GCM-SHA384",
	"ECDHE-RSA-AES256-GCM-SHA384",
}

var fipsGoCiphers = []uint16{
	gotls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	gotls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	gotls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	gotls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
}

func index(ciphers []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, cipher := range ciphers {
		out[cipher] = struct{}{}
	}
	return out
}

var fipsCipherIndex = index(fipsCiphers)

// EnforceGoCompliance limits the TLS settings to the compliant values.
// This should be called as the last policy.
func EnforceGoCompliance(ctx *gotls.Config) {
	switch common_features.CompliancePolicy {
	case "":
		return
	case common_features.FIPS_140_2:
		ctx.MinVersion = gotls.VersionTLS12
		ctx.MaxVersion = gotls.VersionTLS12
		ctx.CipherSuites = fipsGoCiphers
		ctx.CurvePreferences = []gotls.CurveID{gotls.CurveP256}
		return
	case common_features.FIPS_202205:
		// Mirror the settings from Envoy
		// https://github.com/envoyproxy/envoy/blob/a0c9c494a4405d78de15c246c0b915d83612f81a/api/envoy/extensions/transport_sockets/tls/v3/common.proto#L49
		ctx.GetConfigForClient = func(chi *gotls.ClientHelloInfo) (*gotls.Config, error) {
			cfg := &gotls.Config{
				CurvePreferences: []gotls.CurveID{gotls.CurveP256, gotls.CurveP384},
				MinVersion:       gotls.VersionTLS12,
				MaxVersion:       gotls.VersionTLS13,
			}
			validSignatureSchemes := map[gotls.SignatureScheme]struct{}{
				gotls.PKCS1WithSHA256:        {},
				gotls.PKCS1WithSHA384:        {},
				gotls.PKCS1WithSHA512:        {},
				gotls.PSSWithSHA256:          {},
				gotls.PSSWithSHA384:          {},
				gotls.PSSWithSHA512:          {},
				gotls.ECDSAWithP256AndSHA256: {},
				gotls.ECDSAWithP384AndSHA384: {},
			}
			for _, signature := range chi.SignatureSchemes {
				if _, ok := validSignatureSchemes[signature]; !ok {
					return nil, errors.New("unsupported signature scheme")
				}
			}
			greatestVersion := slices.Max(chi.SupportedVersions)
			if greatestVersion == gotls.VersionTLS12 {
				cfg.MaxVersion = gotls.VersionTLS12
				cfg.CipherSuites = fipsGoCiphers
			}

			return cfg, nil
		}

	default:
		log.Warnf("unknown compliance policy: %q", common_features.CompliancePolicy)
		return
	}
}

// EnforceCompliance limits the TLS settings to the compliant values.
// This should be called as the last policy.
func EnforceCompliance(ctx *tls.CommonTlsContext) {
	switch common_features.CompliancePolicy {
	case "":
		return
	case common_features.FIPS_140_2:
		if ctx.TlsParams == nil {
			ctx.TlsParams = &tls.TlsParameters{}
		}
		ctx.TlsParams.TlsMinimumProtocolVersion = tls.TlsParameters_TLSv1_2
		ctx.TlsParams.TlsMaximumProtocolVersion = tls.TlsParameters_TLSv1_2
		// Default (unset) cipher suites field in the FIPS build of Envoy uses only the FIPS ciphers.
		// Therefore, we only filter this field when it is set.
		if len(ctx.TlsParams.CipherSuites) > 0 {
			ciphers := []string{}
			for _, cipher := range ctx.TlsParams.CipherSuites {
				if _, ok := fipsCipherIndex[cipher]; ok {
					ciphers = append(ciphers, cipher)
				}
			}
			ctx.TlsParams.CipherSuites = ciphers
		}
		// Default (unset) is P-256
		ctx.TlsParams.EcdhCurves = nil
		return
	case common_features.FIPS_202205:
		if ctx.TlsParams == nil {
			ctx.TlsParams = &tls.TlsParameters{}
		}
		ctx.TlsParams.CompliancePolicies = []tls.TlsParameters_CompliancePolicy{
			tls.TlsParameters_FIPS_202205,
		}
	default:
		log.Warnf("unknown compliance policy: %q", common_features.CompliancePolicy)
		return
	}
}
