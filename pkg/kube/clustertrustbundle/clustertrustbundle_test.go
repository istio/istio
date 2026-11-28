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

package clustertrustbundle

import (
	"encoding/pem"
	"strings"
	"testing"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube/clustertrustbundle/clustertrustbundletest"
	"istio.io/istio/pkg/test/util/assert"
)

func TestParseTrustBundle(t *testing.T) {
	ca1 := clustertrustbundletest.GenerateCA(t, "ca1")
	ca2 := clustertrustbundletest.GenerateCA(t, "ca2")
	keyOnly := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not a cert")}))

	cases := []struct {
		name    string
		bundle  *certificatesv1.ClusterTrustBundle
		want    string
		wantErr string
	}{
		{name: "nil", bundle: nil, wantErr: "nil"},
		{name: "single cert", bundle: ctb(ca1), want: ca1},
		{name: "multiple certs", bundle: ctb(ca1 + ca2), want: ca1 + ca2},
		{name: "empty", bundle: ctb(""), wantErr: "empty"},
		{name: "whitespace", bundle: ctb(" \n\t\n"), wantErr: "empty"},
		{name: "garbage", bundle: ctb("this is not pem"), wantErr: "valid PEM"},
		{name: "non-certificate block", bundle: ctb(keyOnly), wantErr: "valid PEM"},
		{
			name:    "invalid certificate contents",
			bundle:  ctb(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("junk")}))),
			wantErr: "valid PEM",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTrustBundle(tt.bundle)
			if tt.wantErr != "" {
				assert.Error(t, err)
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, string(got), tt.want)
		})
	}
}

func ctb(bundle string) *certificatesv1.ClusterTrustBundle {
	return &certificatesv1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{Name: "bundle"},
		Spec:       certificatesv1.ClusterTrustBundleSpec{TrustBundle: bundle},
	}
}
