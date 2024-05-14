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

package manifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/test/env"
)

const operatorSubdirFilePath = "manifests"

func TestReadLayeredYAMLs(t *testing.T) {
	testDataDir := filepath.Join(env.IstioSrc, "operator/pkg/util/testdata/yaml")
	tests := []struct {
		name     string
		overlays []string
		wantErr  bool
		stdin    bool
	}{
		{
			name:     "layer1",
			overlays: []string{"yaml_layer1"},
			wantErr:  false,
		},
		{
			name:     "layer1_stdin",
			overlays: []string{"yaml_layer1"},
			wantErr:  false,
			stdin:    true,
		},
		{
			name:     "layer1_2",
			overlays: []string{"yaml_layer1", "yaml_layer2"},
			wantErr:  false,
		},
		{
			name:     "layer1_2_3",
			overlays: []string{"yaml_layer1", "yaml_layer2", "yaml_layer3"},
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s stdin=%v", tt.name, tt.stdin), func(t *testing.T) {
			inDir := filepath.Join(testDataDir, "input")
			outPath := filepath.Join(testDataDir, "output", tt.name+".yaml")
			wantBytes, err := os.ReadFile(outPath)
			want := string(wantBytes)
			if err != nil {
				t.Errorf("os.ReadFile() error = %v, filename: %v", err, outPath)
			}

			stdinReader := &bytes.Buffer{}

			var filenames []string
			for _, ol := range tt.overlays {
				filename := filepath.Join(inDir, ol+".yaml")
				if tt.stdin {
					b, err := os.ReadFile(filename)
					if err != nil {
						t.Fatalf("os.ReadFile() error = %v, filenaem: %v", err, filename)
					}
					if _, err := stdinReader.Write(b); err != nil {
						t.Fatalf("failed to populate fake sdtin")
					}
					filenames = append(filenames, "-")
				} else {
					filenames = append(filenames, filename)
				}
			}
			got, err := readLayeredYAMLs(filenames, stdinReader)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadLayeredYAMLs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			diff := util.YAMLDiff(got, want)
			if diff != "" {
				t.Errorf("ReadLayeredYAMLs() got:\n%s\nwant:\n%s\ndiff:\n%s", got, want, diff)
			}
		})
	}
}

func TestConvertIOPMapValues(t *testing.T) {
	testDataDir := filepath.Join(env.IstioSrc, "operator/pkg/util/testdata/yaml")
	tests := []struct {
		name         string
		inputFlags   []string
		convertPaths []string
	}{
		{
			name:         "convention_boolean",
			convertPaths: defaultSetFlagConvertPaths,
			inputFlags: []string{
				"meshConfig.defaultConfig.proxyMetadata.ISTIO_DUAL_STACK=false",
				"meshConfig.defaultConfig.proxyMetadata.PROXY_XDS_VIA_AGENT=false",
			},
		}, {
			name:         "convention_integer",
			convertPaths: defaultSetFlagConvertPaths,
			inputFlags: []string{
				"meshConfig.defaultConfig.proxyMetadata.ISTIO_MULTI_CLUSTERS=10",
				"meshConfig.defaultConfig.proxyMetadata.PROXY_XDS_LISTENERS=20",
			},
		}, {
			name:         "convention_float",
			convertPaths: defaultSetFlagConvertPaths,
			inputFlags: []string{
				"meshConfig.defaultConfig.proxyMetadata.PROXY_UPSTREAM_WEIGHT=0.85",
				"meshConfig.defaultConfig.proxyMetadata.PROXY_DOWNSTREAM_WEIGHT=0.15",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inPath := filepath.Join(testDataDir, "input", tt.name+".yaml")
			outPath := filepath.Join(testDataDir, "output", tt.name+".yaml")
			input, err := os.ReadFile(inPath)
			if err != nil {
				t.Fatalf(err.Error())
			}
			actualOutput, err := convertIOPMapValues(string(input), tt.inputFlags, tt.convertPaths)
			if err != nil {
				t.Fatalf(err.Error())
			}
			expectOutput, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf(err.Error())
			}

			diff := util.YAMLDiff(actualOutput, string(expectOutput))
			if diff != "" {
				t.Errorf("convertIOPMapValues() got:\n%s\nwant:\n%s\ndiff:\n%s", actualOutput, string(expectOutput), diff)
			}
		})
	}
}

func TestGenIOPFromProfile(t *testing.T) {
	tests := []struct {
		name        string
		setFlags    []string
		expectError bool
	}{
		{
			name:     "cpu_limits",
			setFlags: []string{"values.global.proxy.resources.limits.cpu=200"},
		},
		{
			name:     "compatibilityVersion",
			setFlags: []string{"values.compatibilityVersion=1.20", "compatibilityVersion=1.20"},
		},
		{
			name:     "ingressGateways",
			setFlags: []string{"components.ingressGateways[0].enabled=true", "components.ingressGateways[0].name=test"},
		},
		{
			name:     "egressGatewayJsonNotation",
			setFlags: []string{"values.gateways.istio-egressgateway.enabled=true"},
		},
		{
			name:        "nonexistant_field",
			setFlags:    []string{"values.global.proxy.unknown.image=test"},
			expectError: true,
		},
	}
	manifests := filepath.Join(env.IstioSrc, operatorSubdirFilePath)
	profiles, err := helm.ListProfiles(manifests)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) < 2 {
		// Just ensure we find some profiles, in case this code breaks
		t.Fatalf("Maybe have failed getting profiles, got %v", profiles)
	}
	l := clog.NewConsoleLogger(os.Stdout, os.Stderr, nil)
	for _, profile := range profiles {
		for _, tc := range tests {
			t.Run(profile+"_"+tc.name, func(t *testing.T) {
				_, _, err := GenIOPFromProfile(profile, "", append([]string{"installPackagePath=" + manifests}, tc.setFlags...), false, false, nil, l)
				if (err != nil) != tc.expectError {
					t.Fatal(err)
				}
			})
		}
	}
}
