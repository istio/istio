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
	"crypto/fips140"
	gotls "crypto/tls"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	common_features "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/log"
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
	case common_features.FIPS_140_3:
		if !fips140.Enabled() {
			log.Warnf("COMPLIANCE_POLICY is set to %q but Go FIPS 140-3 mode is not enabled. "+
				"TLS 1.3 cipher suites will not be restricted to FIPS-approved algorithms. "+
				"Set GODEBUG=fips140=only to enable full FIPS 140-3 compliance.", common_features.FIPS_140_3)
		}
		ctx.MinVersion = gotls.VersionTLS12
		ctx.MaxVersion = gotls.VersionTLS13
		// CipherSuites only controls TLS 1.2 cipher selection. TLS 1.3 cipher suites
		// are not configurable via tls.Config and are restricted to FIPS-approved
		// algorithms (AES-GCM only) by Go's crypto library when GODEBUG=fips140=only.
		ctx.CipherSuites = fipsGoCiphers
		ctx.CurvePreferences = []gotls.CurveID{gotls.CurveP256, gotls.CurveP384}
		return
	case common_features.PQC:
		ctx.MinVersion = gotls.VersionTLS13
		ctx.CipherSuites = []uint16{gotls.TLS_AES_128_GCM_SHA256, gotls.TLS_AES_256_GCM_SHA384}
		ctx.CurvePreferences = []gotls.CurveID{gotls.X25519MLKEM768}
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
	case common_features.FIPS_140_3:
		if ctx.TlsParams == nil {
			ctx.TlsParams = &tls.TlsParameters{}
		}
		// Use Envoy's native FIPS_202205 compliance policy which enforces:
		// - TLS 1.2 or 1.3
		// - For TLS 1.2, only ECDHE_[RSA|ECDSA]_WITH_AES_*_GCM_SHA*
		// - For TLS 1.3, only AES-GCM
		// - P-256 or P-384 for key agreement
		ctx.TlsParams.CompliancePolicies = []tls.TlsParameters_CompliancePolicy{tls.TlsParameters_FIPS_202205}
		return
	case common_features.PQC:
		if ctx.TlsParams == nil {
			ctx.TlsParams = &tls.TlsParameters{}
		}
		ctx.TlsParams.TlsMinimumProtocolVersion = tls.TlsParameters_TLSv1_3
		// Explicit cipher suites for TLS v1.3 are required by Envoy OpenSSL
		// to ensure that only TLS_AES_128_GCM_SHA256 and TLS_AES_256_GCM_SHA384 are used.
		ctx.TlsParams.CipherSuites = fipsCiphers
		ctx.TlsParams.EcdhCurves = []string{"X25519MLKEM768"}
		return
	default:
		log.Warnf("unknown compliance policy: %q", common_features.CompliancePolicy)
		return
	}
}
