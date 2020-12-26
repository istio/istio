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
	"crypto/x509/pkix"
	"encoding/asn1"
)

var (
	oidKeyUsage    = asn1.ObjectIdentifier{2, 5, 29, 15}
	oidExtKeyUsage = asn1.ObjectIdentifier{2, 5, 29, 37}
)

func workloadCSRValidateKeyUsage(ext pkix.Extension) bool {
	return true
}

func workloadCSRValidateExtKeyUsage(ext pkix.Extension) bool {
	return true
}

// WorkloadCSRValidateExt : Check for Illegal extentions in workload CSR submitted by Istio workloads
func WorkloadCSRValidateExt(csrExt []pkix.Extension) bool {
	for _, ext := range csrExt {
		if !ext.Id.Equal(oidSubjectAlternativeName) ||
			!ext.Id.Equal(oidExtKeyUsage) ||
			!ext.Id.Equal(oidKeyUsage) {
			return false
		} else if ext.Id.Equal(oidKeyUsage) && !workloadCSRValidateKeyUsage(ext) {
			return false
		} else if ext.Id.Equal(oidExtKeyUsage) && !workloadCSRValidateExtKeyUsage(ext) {
			return false
		}
	}
	return true
}
