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

package grpcgen

import (
	"testing"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
)

// TestBuildCommonTLSContext_BothFieldsSet verifies that buildCommonTLSContext sends both
// current and deprecated xDS certificate provider fields for backward compatibility.
//
// This ensures compatibility with:
// - Older gRPC clients (grpc-go < 1.66, grpc-cpp < 1.66, older grpc-java) that use deprecated fields
// - Newer gRPC clients (grpc-go >= 1.66, grpc-cpp >= 1.66, modern grpc-java) that use current fields
//
// Related:
// - https://github.com/grpc/grpc-java/pull/12435
// - https://github.com/grpc/grpc-java/commit/65d0bb8a4d9ee111f1eeb02c43321bbb99143151
func TestBuildCommonTLSContext_BothFieldsSet(t *testing.T) {
	// Test with SAN matching
	sans := []string{"spiffe://trust-domain/ns/default/sa/service"}
	ctx := buildCommonTLSContext(sans)

	// Verify current identity certificate field (field 14) is set
	if ctx.TlsCertificateProviderInstance == nil {
		t.Error("TlsCertificateProviderInstance (current field 14) should be set")
	} else {
		if ctx.TlsCertificateProviderInstance.InstanceName != "default" {
			t.Errorf("Expected instance name 'default', got %s", ctx.TlsCertificateProviderInstance.InstanceName)
		}
		if ctx.TlsCertificateProviderInstance.CertificateName != "default" {
			t.Errorf("Expected certificate name 'default', got %s", ctx.TlsCertificateProviderInstance.CertificateName)
		}
	}

	// Verify deprecated identity certificate field (field 11) is still set for backward compatibility
	//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
	if ctx.TlsCertificateCertificateProviderInstance == nil {
		t.Error("TlsCertificateCertificateProviderInstance (deprecated field 11) should be set")
	} else {
		//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
		if ctx.TlsCertificateCertificateProviderInstance.InstanceName != "default" {
			//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
			t.Errorf("Expected instance 'default', got %s", ctx.TlsCertificateCertificateProviderInstance.InstanceName)
		}
		//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
		if ctx.TlsCertificateCertificateProviderInstance.CertificateName != "default" {
			//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
			t.Errorf("Expected cert 'default', got %s", ctx.TlsCertificateCertificateProviderInstance.CertificateName)
		}
	}

	// Verify the validation context type is CombinedValidationContext
	combined, ok := ctx.ValidationContextType.(*tls.CommonTlsContext_CombinedValidationContext)
	if !ok {
		t.Fatal("ValidationContextType should be CombinedValidationContext")
	}

	// Verify current CA certificate field is set
	if combined.CombinedValidationContext.DefaultValidationContext == nil {
		t.Fatal("DefaultValidationContext should be set")
	}
	if combined.CombinedValidationContext.DefaultValidationContext.CaCertificateProviderInstance == nil {
		t.Error("CaCertificateProviderInstance (current field) should be set in DefaultValidationContext")
	} else {
		ca := combined.CombinedValidationContext.DefaultValidationContext.CaCertificateProviderInstance
		if ca.InstanceName != "default" {
			t.Errorf("Expected CA instance name 'default', got %s", ca.InstanceName)
		}
		if ca.CertificateName != "ROOTCA" {
			t.Errorf("Expected CA certificate name 'ROOTCA', got %s", ca.CertificateName)
		}
	}

	// Verify deprecated CA certificate field (field 4) is still set for backward compatibility
	//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
	if combined.CombinedValidationContext.ValidationContextCertificateProviderInstance == nil {
		t.Error("ValidationContextCertificateProviderInstance (deprecated field 4) should be set")
	} else {
		//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
		ca := combined.CombinedValidationContext.ValidationContextCertificateProviderInstance
		if ca.InstanceName != "default" {
			t.Errorf("Expected CA instance 'default', got %s", ca.InstanceName)
		}
		if ca.CertificateName != "ROOTCA" {
			t.Errorf("Expected CA cert 'ROOTCA', got %s", ca.CertificateName)
		}
	}

	// Verify SAN matching is set
	//nolint:staticcheck // SA1019: MatchSubjectAltNames is deprecated but still functional
	if combined.CombinedValidationContext.DefaultValidationContext.MatchSubjectAltNames == nil {
		t.Error("MatchSubjectAltNames should be set when SANs are provided")
	}
	//nolint:staticcheck // SA1019: MatchSubjectAltNames is deprecated but still functional
	if len(combined.CombinedValidationContext.DefaultValidationContext.MatchSubjectAltNames) != 1 {
		sans := combined.CombinedValidationContext.DefaultValidationContext.MatchSubjectAltNames
		//nolint:staticcheck // SA1019: MatchSubjectAltNames is deprecated but still functional
		t.Errorf("Expected 1 SAN matcher, got %d", len(sans))
	}
}

// TestBuildCommonTLSContext_NoSANs verifies the function works correctly when no SANs are provided
func TestBuildCommonTLSContext_NoSANs(t *testing.T) {
	ctx := buildCommonTLSContext(nil)

	// Basic checks that both fields are still set
	if ctx.TlsCertificateProviderInstance == nil {
		t.Error("TlsCertificateProviderInstance (current) should be set even without SANs")
	}
	//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
	if ctx.TlsCertificateCertificateProviderInstance == nil {
		t.Error("TlsCertificateCertificateProviderInstance should be set")
	}

	// Verify validation context
	combined, ok := ctx.ValidationContextType.(*tls.CommonTlsContext_CombinedValidationContext)
	if !ok {
		t.Fatal("ValidationContextType should be CombinedValidationContext")
	}

	if combined.CombinedValidationContext.DefaultValidationContext == nil {
		t.Fatal("DefaultValidationContext should be set")
	}

	// Verify current CA field is set
	if combined.CombinedValidationContext.DefaultValidationContext.CaCertificateProviderInstance == nil {
		t.Error("CaCertificateProviderInstance (current) should be set even without SANs")
	}

	// Verify deprecated CA field is set
	//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
	if combined.CombinedValidationContext.ValidationContextCertificateProviderInstance == nil {
		t.Error("ValidationContextCertificateProviderInstance should be set")
	}

	// Verify SAN matching is nil when no SANs provided
	//nolint:staticcheck // SA1019: MatchSubjectAltNames is deprecated but still functional
	if combined.CombinedValidationContext.DefaultValidationContext.MatchSubjectAltNames != nil {
		t.Error("MatchSubjectAltNames should be nil when no SANs are provided")
	}
}

// TestBuildCommonTLSContext_FieldValuesMatch verifies that both deprecated and current
// fields have matching values (even though they're different types)
func TestBuildCommonTLSContext_FieldValuesMatch(t *testing.T) {
	ctx := buildCommonTLSContext(nil)

	// Note: The fields use different proto types:
	// - TlsCertificateProviderInstance uses CertificateProviderPluginInstance
	// - TlsCertificateCertificateProviderInstance uses CommonTlsContext_CertificateProviderInstance
	// So we can only compare their values, not the pointers

	// Verify both identity fields have the same values
	current := ctx.TlsCertificateProviderInstance
	//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
	deprecated := ctx.TlsCertificateCertificateProviderInstance
	if current.InstanceName != deprecated.InstanceName {
		t.Error("Identity certificate instance names should match")
	}
	if current.CertificateName != deprecated.CertificateName {
		t.Error("Identity certificate names should match")
	}

	// Verify CA certificate fields have matching values
	combined := ctx.ValidationContextType.(*tls.CommonTlsContext_CombinedValidationContext)
	currentCA := combined.CombinedValidationContext.DefaultValidationContext.CaCertificateProviderInstance
	//nolint:staticcheck // SA1019: intentionally testing deprecated field for backward compatibility
	deprecatedCA := combined.CombinedValidationContext.ValidationContextCertificateProviderInstance

	// Similarly, these use different types so we only compare values
	if currentCA.InstanceName != deprecatedCA.InstanceName {
		t.Error("CA certificate instance names should match")
	}
	if currentCA.CertificateName != deprecatedCA.CertificateName {
		t.Error("CA certificate names should match")
	}
}
