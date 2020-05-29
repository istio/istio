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

package helper

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	gapiopts "google.golang.org/api/option"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	testenv "istio.io/istio/mixer/pkg/adapter/test"
)

func TestToOpts(t *testing.T) {
	testSvcAcctFile, err := ioutil.TempFile("", "serviceAccount.json")
	if err != nil {
		t.Fatalf("cannot create a temp file for testing: %v", err)
	}
	svcAcctFilePath := testSvcAcctFile.Name()
	defer os.Remove(svcAcctFilePath)
	tests := []struct {
		name string
		cfg  *config.Params
		out  []gapiopts.ClientOption // we only assert that the types match, so contents of the option don't matter
	}{
		{"empty", &config.Params{}, []gapiopts.ClientOption{}},
		{"api key", &config.Params{Creds: &config.Params_ApiKey{}}, []gapiopts.ClientOption{}},
		{"app creds", &config.Params{Creds: &config.Params_AppCredentials{}}, []gapiopts.ClientOption{}},
		{"service account",
			&config.Params{Creds: &config.Params_ServiceAccountPath{ServiceAccountPath: svcAcctFilePath}},
			[]gapiopts.ClientOption{gapiopts.WithCredentialsFile(svcAcctFilePath)}},
		{"service account file not found",
			&config.Params{Creds: &config.Params_ServiceAccountPath{ServiceAccountPath: "/some/non/existent/path"}},
			[]gapiopts.ClientOption{}},
		{"service account empty",
			&config.Params{Creds: &config.Params_ServiceAccountPath{}},
			[]gapiopts.ClientOption{}},
		{"endpoint",
			&config.Params{Endpoint: "foo.bar"},
			[]gapiopts.ClientOption{gapiopts.WithEndpoint("")}},
		{"endpoint + svc account",
			&config.Params{Endpoint: "foo.bar", Creds: &config.Params_ServiceAccountPath{}},
			[]gapiopts.ClientOption{gapiopts.WithEndpoint("")}},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			opts := ToOpts(tt.cfg, testenv.NewEnv(t).Logger())
			if len(opts) != len(tt.out) {
				t.Errorf("len(toOpts(%v)) = %d, expected %d", tt.cfg, len(opts), len(tt.out))
			}

			optSet := make(map[gapiopts.ClientOption]struct{})
			for _, opt := range opts {
				optSet[opt] = struct{}{}
			}

			for _, expected := range tt.out {
				found := false
				for _, actual := range opts {
					// We care that the types are what we expect, not necessarily that they're identical
					found = found || (reflect.TypeOf(expected) == reflect.TypeOf(actual))
				}
				if !found {
					t.Errorf("toOpts() = %v, wanted opt '%v' (type %v)", opts, expected, reflect.TypeOf(expected))
				}
			}
		})
	}
}

func TestMetadata(t *testing.T) {
	tests := []struct {
		name          string
		shouldFill    shouldFillFn
		projectIDFn   metadataFn
		locationFn    metadataFn
		clusterNameFn metadataFn
		meshIDFn      metadataFn
		want          Metadata
	}{
		{
			"should not fill",
			func() bool { return false },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "mesh-id", nil },
			Metadata{ProjectID: "", Location: "", ClusterName: ""},
		},
		{
			"should fill",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "mesh-id", nil },
			Metadata{ProjectID: "pid", Location: "location", ClusterName: "cluster"},
		},
		{
			"project id error",
			func() bool { return true },
			func() (string, error) { return "", errors.New("error") },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "mesh-id", nil },
			Metadata{ProjectID: "", Location: "location", ClusterName: "cluster"},
		},
		{
			"location error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "location", errors.New("error") },
			func() (string, error) { return "cluster", nil },
			func() (string, error) { return "mesh-id", nil },
			Metadata{ProjectID: "pid", Location: "", ClusterName: "cluster"},
		},
		{
			"cluster name error",
			func() bool { return true },
			func() (string, error) { return "pid", nil },
			func() (string, error) { return "location", nil },
			func() (string, error) { return "cluster", errors.New("error") },
			func() (string, error) { return "mesh-id", nil },
			Metadata{ProjectID: "pid", Location: "location", ClusterName: ""},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			mg := NewMetadataGenerator(tt.shouldFill, tt.projectIDFn, tt.locationFn, tt.clusterNameFn)
			got := mg.GenerateMetadata()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Unexpected generated metadata: want %v got %v", tt.want, got)
			}
		})
	}
}

func TestFillProjectMetadata(t *testing.T) {
	tests := []struct {
		name string
		md   Metadata
		in   map[string]string
		want map[string]string
	}{
		{
			"empty metadata",
			Metadata{ProjectID: "", Location: "", ClusterName: ""},
			map[string]string{"project_id": "pid", "location": "location", "cluster_name": ""},
			map[string]string{"project_id": "pid", "location": "location", "cluster_name": ""},
		},
		{
			"fill metadata",
			Metadata{ProjectID: "pid", Location: "location", ClusterName: "cluster"},
			map[string]string{"project_id": "", "location": "location", "cluster_name": ""},
			map[string]string{"project_id": "pid", "location": "location", "cluster_name": "cluster"},
		},
		{
			"do not override",
			Metadata{ProjectID: "pid", Location: "location", ClusterName: "cluster"},
			map[string]string{"project_id": "id", "location": "l", "cluster_name": "c"},
			map[string]string{"project_id": "id", "location": "l", "cluster_name": "c"},
		},
		{
			"unrelated field",
			Metadata{ProjectID: "", Location: "", ClusterName: "cluster"},
			map[string]string{"project_id": "pid", "location": "location", "cluster": ""},
			map[string]string{"project_id": "pid", "location": "location", "cluster": ""},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			tt.md.FillProjectMetadata(tt.in)
			if !reflect.DeepEqual(tt.in, tt.want) {
				t.Errorf("Unexpected map value, want %v got %v", tt.want, tt.in)
			}
		})
	}
}
