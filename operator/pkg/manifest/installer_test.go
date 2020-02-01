// Copyright 2020 Istio Authors
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

package manifest

import (
	"reflect"
	"testing"

	goversion "github.com/hashicorp/go-version"
)

func Test_parseKubectlVersion(t *testing.T) {
	ver1170, _ := goversion.NewVersion("1.17.0")
	ver1157, _ := goversion.NewVersion("1.15.7")
	tests := []struct {
		name          string
		kubectlStdout string
		wantClientVer *goversion.Version
		wantServerVer *goversion.Version
		wantErr       bool
	}{
		{
			name: "client version and server version",
			kubectlStdout: `
clientVersion:
  buildDate: "2019-12-07T21:20:10Z"
  compiler: gc
  gitCommit: 70132b0f130acc0bed193d9ba59dd186f0e634cf
  gitTreeState: clean
  gitVersion: v1.17.0
  goVersion: go1.13.4
  major: "1"
  minor: "17"
  platform: linux/amd64
serverVersion:
  buildDate: "2019-12-13T12:37:28Z"
  compiler: gc
  gitCommit: 96c91b15a936b36701e8704844bc356862440a21
  gitTreeState: clean
  gitVersion: v1.15.7
  goVersion: go1.12.12b4
  major: "1"
  minor: 15+
  platform: linux/amd64`,
			wantErr:       false,
			wantClientVer: ver1170,
			wantServerVer: ver1157,
		},
		{
			name: "client version and server version",
			kubectlStdout: `
clientVersion:
  buildDate: "2019-12-07T21:20:10Z"
  compiler: gc
  gitCommit: 70132b0f130acc0bed193d9ba59dd186f0e634cf
  gitTreeState: clean
  gitVersion: v1.17.0
  goVersion: go1.13.4
  major: "1"
  minor: "17"
  platform: linux/amd64`,
			wantErr:       false,
			wantClientVer: ver1170,
			wantServerVer: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClientVer, gotServerVer, err := parseKubectlVersion(tt.kubectlStdout)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseKubectlVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotClientVer, tt.wantClientVer) {
				t.Errorf("Version() got ClientVer = %v, want ClientVer %v", gotClientVer, tt.wantClientVer)
			}
			if !reflect.DeepEqual(gotServerVer, tt.wantServerVer) {
				t.Errorf("Version() got ServerVer = %v, want ServerVer %v", gotServerVer, tt.wantServerVer)
			}
		})
	}
}
