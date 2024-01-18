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
	"fmt"
	"golang.org/x/sys/unix"
	"istio.io/istio/pkg/test/util/file"
	"os"
	"testing"

	utilversion "k8s.io/apimachinery/pkg/util/version"

	"istio.io/istio/pkg/test/util/assert"
)

func TestUnshare(t *testing.T) {
	t.Log(file.MustAsString("/proc/mounts"))
	err := DoUnshare(func() error {
		t.Log(file.MustAsString("/proc/mounts"))
		if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
			return &os.PathError{Op: "mount", Path: "/", Err: err}
		}
		if err := mount("/tmp/a", "/tmp/b"); err != nil {
			return fmt.Errorf("bind mount of %q failed: %v", "/tmp/a", err)
		}
		t.Log(file.MustAsString("/proc/mounts"))
		t.Log(file.MustAsString("/tmp/b"))
		return nil
	})
		t.Log(file.MustAsString("/tmp/b"))
	assert.NoError(t, err)
}

func TestDetectIptablesVersion(t *testing.T) {
	cases := []struct {
		name string
		ver  string
		want IptablesVersion
	}{
		{
			name: "jammy nft",
			ver:  "iptables v1.8.7 (nf_tables)",
			want: IptablesVersion{version: utilversion.MustParseGeneric("1.8.7"), legacy: false},
		},
		{
			name: "jammy legacy",
			ver:  "iptables v1.8.7 (legacy)",

			want: IptablesVersion{version: utilversion.MustParseGeneric("1.8.7"), legacy: true},
		},
		{
			name: "xenial",
			ver:  "iptables v1.6.0",

			want: IptablesVersion{version: utilversion.MustParseGeneric("1.6.0"), legacy: true},
		},
		{
			name: "bionic",
			ver:  "iptables v1.6.1",

			want: IptablesVersion{version: utilversion.MustParseGeneric("1.6.1"), legacy: true},
		},
		{
			name: "centos 7",
			ver:  "iptables v1.4.21",

			want: IptablesVersion{version: utilversion.MustParseGeneric("1.4.21"), legacy: true},
		},
		{
			name: "centos 8",
			ver:  "iptables v1.8.4 (nf_tables)",

			want: IptablesVersion{version: utilversion.MustParseGeneric("1.8.4"), legacy: false},
		},
		{
			name: "alpine 3.18",
			ver:  "iptables v1.8.9 (legacy)",

			want: IptablesVersion{version: utilversion.MustParseGeneric("1.8.9"), legacy: true},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectIptablesVersion(tt.ver)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, got.version.String(), tt.want.version.String())
			assert.Equal(t, got.legacy, tt.want.legacy)
		})
	}
}
