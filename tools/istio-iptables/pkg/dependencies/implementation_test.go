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

package dependencies

import (
	"testing"

	utilversion "k8s.io/apimachinery/pkg/util/version"

	"istio.io/istio/pkg/test/util/assert"
)

func TestOverrideVersionIsCorrectlyParsed(t *testing.T) {
	cases := []struct {
		name string
		ver  string
		want *utilversion.Version
	}{
		{
			name: "jammy nft",
			ver:  "iptables v1.8.7 (nf_tables)",
			want: utilversion.MustParseGeneric("1.8.7"),
		},
		{
			name: "jammy legacy",
			ver:  "iptables v1.8.7 (legacy)",

			want: utilversion.MustParseGeneric("1.8.7"),
		},
		{
			name: "xenial",
			ver:  "iptables v1.6.0",

			want: utilversion.MustParseGeneric("1.6.0"),
		},
		{
			name: "bionic",
			ver:  "iptables v1.6.1",

			want: utilversion.MustParseGeneric("1.6.1"),
		},
		{
			name: "centos 7",
			ver:  "iptables v1.4.21",

			want: utilversion.MustParseGeneric("1.4.21"),
		},
		{
			name: "centos 8",
			ver:  "iptables v1.8.4 (nf_tables)",

			want: utilversion.MustParseGeneric("1.8.4"),
		},
		{
			name: "alpine 3.18",
			ver:  "iptables v1.8.9 (legacy)",

			want: utilversion.MustParseGeneric("1.8.9"),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIptablesVer(tt.ver)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, got.String(), tt.want.String())
		})
	}
}
