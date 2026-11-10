// Copyright Istio Authors.
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

package workload

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

var fakeCACert = []byte("fake-CA-cert")

var (
	defaultYAML = `apiVersion: networking.istio.io/v1
kind: WorkloadGroup
metadata:
  name: foo
  namespace: bar
spec:
  metadata: {}
  template:
    serviceAccount: default
`

	customYAML = `apiVersion: networking.istio.io/v1
kind: WorkloadGroup
metadata:
  name: foo
  namespace: bar
spec:
  metadata:
    annotations:
      annotation: foobar
    labels:
      app: foo
      bar: baz
  template:
    ports:
      grpc: 3550
      http: 8080
    serviceAccount: test
    weight: 100
`
)

type testcase struct {
	description       string
	expectedException bool
	args              []string
	expectedOutput    string
	namespace         string
}

func TestWorkloadGroupCreate(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args - missing service name and namespace",
			args:              strings.Split("group create", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a workload name\n",
		},
		{
			description:       "Invalid command args - missing service name",
			args:              strings.Split("group create -n bar", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a workload name\n",
		},
		{
			description:       "Invalid command args - missing service namespace",
			args:              strings.Split("group create --name foo", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a workload namespace\n",
		},
		{
			description:       "valid case - minimal flags, infer defaults",
			args:              strings.Split("group create --name foo --namespace bar", " "),
			expectedException: false,
			expectedOutput:    defaultYAML,
		},
		{
			description: "valid case - create full workload group",
			args: strings.Split("group create --name foo --namespace bar --labels app=foo,bar=baz "+
				" --annotations annotation=foobar --ports grpc=3550,http=8080 --serviceAccount test --weight 100", " "),
			expectedException: false,
			expectedOutput:    customYAML,
		},
		{
			description: "valid case - create full workload group with shortnames",
			args: strings.Split("group create --name foo -n bar -l app=foo,bar=baz -p grpc=3550,http=8080"+
				" -a annotation=foobar --serviceAccount test --weight 100", " "),
			expectedException: false,
			expectedOutput:    customYAML,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyTestcaseOutput(t, Cmd(cli.NewFakeContext(nil)), c)
		})
	}
}

func verifyTestcaseOutput(t *testing.T, cmd *cobra.Command, c testcase) {
	t.Helper()

	var out bytes.Buffer
	cmd.SetArgs(c.args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	if c.namespace != "" {
		namespace = c.namespace
	}

	fErr := cmd.Execute()
	output := out.String()

	if c.expectedException {
		if fErr == nil {
			t.Fatalf("Wanted an exception, "+
				"didn't get one, output was %q", output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception: %v", fErr)
		}
	}

	if c.expectedOutput != "" && c.expectedOutput != output {
		assert.Equal(t, c.expectedOutput, output)
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}
}

func TestWorkloadEntryConfigureInvalidArgs(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args - missing valid input spec",
			args:              strings.Split("entry configure --name foo -o temp --clusterID cid", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a WorkloadGroup artifact file or the name and namespace of an existing WorkloadGroup\n",
		},
		{
			description:       "Invalid command args - missing valid input spec",
			args:              strings.Split("entry configure -n bar -o temp --clusterID cid", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a WorkloadGroup artifact file or the name and namespace of an existing WorkloadGroup\n",
		},
		{
			description:       "Invalid command args - valid filename input but missing output filename",
			args:              strings.Split("entry configure -f file --clusterID cid", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting an output directory\n",
		},
		{
			description:       "Invalid command args - valid kubectl input but missing output filename",
			args:              strings.Split("entry configure --name foo -n bar --clusterID cid", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting an output directory\n",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyTestcaseOutput(t, Cmd(cli.NewFakeContext(nil)), c)
		})
	}
}

var generated = map[string]bool{
	"hosts":         true,
	"istio-token":   true,
	"mesh.yaml":     true,
	"root-cert.pem": true,
	"cluster.env":   true,
}

const goldenSuffix = ".golden"

// TestWorkloadEntryConfigure enumerates test cases based on subdirectories of testdata/vmconfig.
// Each subdirectory contains two input files: workloadgroup.yaml and meshconfig.yaml that are used
// to generate golden outputs from the VM command.
func TestWorkloadEntryConfigure(t *testing.T) {
	noClusterID := "failed to automatically determine the --clusterID"
	files, err := os.ReadDir("testdata/vmconfig")
	if err != nil {
		t.Fatal(err)
	}
	testCases := map[string]struct {
		internalIP string
		ingressIP  string
		extraArgs  []string
	}{
		"ipv4": {
			internalIP: "10.10.10.10",
			ingressIP:  "10.10.10.11",
		},
		"ipv6": {
			internalIP: "fd00:10:96::1",
			ingressIP:  "fd00:10:96::2",
		},
		"overrides": {
			internalIP: "10.10.10.10",
			ingressIP:  "10.10.10.11",
			extraArgs: []string{
				"--overrideConfig", "defaultConfig.binaryPath=/usr/bin/envoy",
				"--overrideConfig", "defaultConfig.concurrency=4",
				"--overrideConfig", "defaultConfig.proxyMetadata.CLUSTER_MESH_CONFIG_VALUE=bar",
				"--overrideConfig", "defaultConfig.proxyMetadata.ISTIO_META_DNS_AUTO_ALLOCATE=true",
				"--overrideEnv", "CA_ADDR=istiod.istio-system.svc.test.com:443",
				"--overrideEnv", "ISTIO_LOCAL_EXCLUDE_PORTS=22,15090,15021,15020,9000",
			},
		},
	}
	for _, dir := range files {
		if !dir.IsDir() {
			continue
		}
		testdir := path.Join("testdata/vmconfig", dir.Name())
		t.Cleanup(func() {
			for k := range generated {
				os.Remove(path.Join(testdir, k))
			}
		})
		t.Run(dir.Name(), func(t *testing.T) {
			createClientFunc := func(client kube.CLIClient) {
				client.Kube().CoreV1().ServiceAccounts("bar").Create(context.Background(), &v1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "vm-serviceaccount"},
					Secrets:    []v1.ObjectReference{{Name: "test"}},
				}, metav1.CreateOptions{})
				client.Kube().CoreV1().ConfigMaps("bar").Create(context.Background(), &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "istio-ca-root-cert"},
					Data:       map[string]string{"root-cert.pem": string(fakeCACert)},
				}, metav1.CreateOptions{})
				client.Kube().CoreV1().ConfigMaps("istio-system").Create(context.Background(), &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: "istio-system", Name: "istio-rev-1"},
					Data: map[string]string{
						"mesh": string(util.ReadFile(t, path.Join(testdir, "meshconfig.yaml"))),
					},
				}, metav1.CreateOptions{})
				client.Kube().CoreV1().Secrets("bar").Create(context.Background(), &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "test"},
					Data: map[string][]byte{
						"token": {},
					},
				}, metav1.CreateOptions{})
			}

			tc := testCases[dir.Name()]

			cmdWithClusterID := []string{
				"entry", "configure",
				"-f", path.Join("testdata/vmconfig", dir.Name(), "workloadgroup.yaml"),
				"--internalIP", tc.internalIP,
				"--ingressIP", tc.ingressIP,
				"--clusterID", constants.DefaultClusterName,
				"--revision", "rev-1",
				"-o", testdir,
			}
			cmdWithClusterID = append(cmdWithClusterID, tc.extraArgs...)
			if _, err := runTestCmd(t, createClientFunc, "rev-1", cmdWithClusterID); err != nil {
				t.Fatal(err)
			}

			cmdNoClusterID := []string{
				"entry", "configure",
				"-f", path.Join("testdata/vmconfig", dir.Name(), "workloadgroup.yaml"),
				"--internalIP", tc.internalIP,
				"--revision", "rev-1",
				"-o", testdir,
			}
			cmdNoClusterID = append(cmdNoClusterID, tc.extraArgs...)
			if output, err := runTestCmd(t, createClientFunc, "rev-1", cmdNoClusterID); err != nil {
				if !strings.Contains(output, noClusterID) {
					t.Fatal(err)
				}
			}

			checkFiles := map[string]bool{
				// inputs that we allow to exist, if other files seep in unexpectedly we fail the test
				".gitignore": false, "meshconfig.yaml": false, "workloadgroup.yaml": false,
			}
			for k, v := range generated {
				checkFiles[k] = v
			}

			checkOutputFiles(t, testdir, checkFiles)
		})
	}
}

func TestWorkloadEntryToPodPortsMeta(t *testing.T) {
	cases := []struct {
		description string
		ports       map[string]uint32
		want        string
	}{
		{
			description: "test json marshal",
			ports: map[string]uint32{
				"HTTP":  80,
				"HTTPS": 443,
			},
			want: `[{"name":"HTTP","containerPort":80,"protocol":""},{"name":"HTTPS","containerPort":443,"protocol":""}]`,
		},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			str := marshalWorkloadEntryPodPorts(c.ports)
			if c.want != str {
				t.Errorf("want %s, got %s", c.want, str)
			}
		})
	}
}

// TestWorkloadEntryConfigureNilProxyMetadata tests a particular use case when the
// proxyMetadata is nil, no metadata would be generated at all.
func TestWorkloadEntryConfigureNilProxyMetadata(t *testing.T) {
	testdir := "testdata/vmconfig-nil-proxy-metadata"
	noClusterID := "failed to automatically determine the --clusterID"

	t.Cleanup(func() {
		for k := range generated {
			os.Remove(path.Join(testdir, k))
		}
	})

	createClientFunc := func(client kube.CLIClient) {
		client.Kube().CoreV1().ServiceAccounts("bar").Create(context.Background(), &v1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "vm-serviceaccount"},
			Secrets:    []v1.ObjectReference{{Name: "test"}},
		}, metav1.CreateOptions{})
		client.Kube().CoreV1().ConfigMaps("bar").Create(context.Background(), &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "istio-ca-root-cert"},
			Data:       map[string]string{"root-cert.pem": string(fakeCACert)},
		}, metav1.CreateOptions{})
		client.Kube().CoreV1().ConfigMaps("istio-system").Create(context.Background(), &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "istio-system", Name: "istio"},
			Data: map[string]string{
				"mesh": "defaultConfig: {}",
			},
		}, metav1.CreateOptions{})
		client.Kube().CoreV1().Secrets("bar").Create(context.Background(), &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "test"},
			Data: map[string][]byte{
				"token": {},
			},
		}, metav1.CreateOptions{})
	}

	cmdWithClusterID := []string{
		"entry", "configure",
		"-f", path.Join(testdir, "workloadgroup.yaml"),
		"--internalIP", "10.10.10.10",
		"--clusterID", constants.DefaultClusterName,
		"-o", testdir,
	}
	if output, err := runTestCmd(t, createClientFunc, "", cmdWithClusterID); err != nil {
		t.Logf("output: %v", output)
		t.Fatal(err)
	}

	cmdNoClusterID := []string{
		"entry", "configure",
		"-f", path.Join(testdir, "workloadgroup.yaml"),
		"--internalIP", "10.10.10.10",
		"-o", testdir,
	}
	if output, err := runTestCmd(t, createClientFunc, "", cmdNoClusterID); err != nil {
		if !strings.Contains(output, noClusterID) {
			t.Fatal(err)
		}
	}

	checkFiles := map[string]bool{
		// inputs that we allow to exist, if other files seep in unexpectedly we fail the test
		".gitignore": false, "workloadgroup.yaml": false,
	}
	for k, v := range generated {
		checkFiles[k] = v
	}

	checkOutputFiles(t, testdir, checkFiles)
}

func runTestCmd(t *testing.T, createResourceFunc func(client kube.CLIClient), rev string, args []string) (string, error) {
	t.Helper()
	// TODO there is already probably something else that does this
	var out bytes.Buffer
	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
		IstioNamespace: "istio-system",
	})
	rootCmd := Cmd(ctx)
	rootCmd.SetArgs(args)
	client, err := ctx.CLIClientWithRevision(ctx.RevisionOrDefault(rev))
	if err != nil {
		return "", err
	}
	createResourceFunc(client)

	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	err = rootCmd.Execute()
	output := out.String()
	return output, err
}

func checkOutputFiles(t *testing.T, testdir string, checkFiles map[string]bool) {
	t.Helper()

	outputFiles, err := os.ReadDir(testdir)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range outputFiles {
		checkGolden, ok := checkFiles[f.Name()]
		if !ok {
			if checkGolden, ok := checkFiles[f.Name()[:len(f.Name())-len(goldenSuffix)]]; !(checkGolden && ok) {
				t.Errorf("unexpected file in output dir: %s", f.Name())
			}
			continue
		}
		if checkGolden {
			t.Run(f.Name(), func(t *testing.T) {
				contents := util.ReadFile(t, path.Join(testdir, f.Name()))
				goldenFile := path.Join(testdir, f.Name()+goldenSuffix)
				util.RefreshGoldenFile(t, contents, goldenFile)
				util.CompareContent(t, contents, goldenFile)
			})
		}
	}
}

func TestConvertToMap(t *testing.T) {
	tests := []struct {
		name string
		arg  []string
		want map[string]string
	}{
		{name: "empty", arg: []string{""}, want: map[string]string{"": ""}},
		{name: "one-valid", arg: []string{"key=value"}, want: map[string]string{"key": "value"}},
		{name: "one-valid-double-equals", arg: []string{"key==value"}, want: map[string]string{"key": "=value"}},
		{name: "one-key-only", arg: []string{"key"}, want: map[string]string{"key": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertToStringMap(tt.arg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertToStringMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseEnvOverrides(t *testing.T) {
	tests := []struct {
		name      string
		overrides []string
		want      map[string]string
		wantErr   string
	}{
		{
			name:      "empty",
			overrides: nil,
			want:      map[string]string{},
		},
		{
			name:      "multiple entries",
			overrides: []string{"CA_ADDR=istiod.istio-system.svc.test.com:443", "ISTIO_SVC_IP=10.10.10.10"},
			want: map[string]string{
				"CA_ADDR":      "istiod.istio-system.svc.test.com:443",
				"ISTIO_SVC_IP": "10.10.10.10",
			},
		},
		{
			name:      "value with commas is not split",
			overrides: []string{"ISTIO_LOCAL_EXCLUDE_PORTS=22,15090"},
			want:      map[string]string{"ISTIO_LOCAL_EXCLUDE_PORTS": "22,15090"},
		},
		{
			name:      "empty value",
			overrides: []string{"CA_ADDR="},
			want:      map[string]string{"CA_ADDR": ""},
		},
		{
			name:      "missing value",
			overrides: []string{"CA_ADDR"},
			wantErr:   "expected the <name>=<value> format",
		},
		{
			name:      "missing name",
			overrides: []string{"=foo"},
			wantErr:   "is not a valid environment variable name",
		},
		{
			name:      "invalid name",
			overrides: []string{"CA ADDR=foo"},
			wantErr:   "is not a valid environment variable name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEnvOverrides(tt.overrides)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected an error containing %q, but got: %v", tt.wantErr, err)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestApplyConfigOverrides(t *testing.T) {
	tests := []struct {
		name      string
		overrides []string
		want      string
		wantErr   string
	}{
		{
			name:      "set a string field",
			overrides: []string{"defaultConfig.binaryPath=/usr/bin/envoy"},
			want: `defaultConfig:
  binaryPath: /usr/bin/envoy
  concurrency: 2
  proxyMetadata:
    POD_NAMESPACE: bar
`,
		},
		{
			name:      "set an integer field",
			overrides: []string{"defaultConfig.concurrency=4"},
			want: `defaultConfig:
  concurrency: 4
  proxyMetadata:
    POD_NAMESPACE: bar
`,
		},
		{
			name:      "proxy metadata values are kept as strings",
			overrides: []string{"defaultConfig.proxyMetadata.ISTIO_META_DNS_CAPTURE=true", "defaultConfig.proxyMetadata.BOOTSTRAP_XDS_AGENT=15020"},
			want: `defaultConfig:
  concurrency: 2
  proxyMetadata:
    BOOTSTRAP_XDS_AGENT: "15020"
    ISTIO_META_DNS_CAPTURE: "true"
    POD_NAMESPACE: bar
`,
		},
		{
			name:      "set a nested field that does not exist yet",
			overrides: []string{"defaultConfig.sds.enabled=true"},
			want: `defaultConfig:
  concurrency: 2
  proxyMetadata:
    POD_NAMESPACE: bar
  sds:
    enabled: true
`,
		},
		{
			name:      "value with an equal sign",
			overrides: []string{"defaultConfig.proxyMetadata.FOO=a=b"},
			want: `defaultConfig:
  concurrency: 2
  proxyMetadata:
    FOO: a=b
    POD_NAMESPACE: bar
`,
		},
		{
			name:      "missing value",
			overrides: []string{"defaultConfig.binaryPath"},
			wantErr:   "expected the <path>=<value> format",
		},
		{
			name:      "missing path",
			overrides: []string{"=/usr/bin/envoy"},
			wantErr:   "the path must not be empty",
		},
		{
			name:      "path traversing a scalar value",
			overrides: []string{"defaultConfig.concurrency.foo=1"},
			wantErr:   "failed to apply --overrideConfig",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meshConfig := values.Map{
				"defaultConfig": map[string]any{
					"concurrency":   2,
					"proxyMetadata": map[string]any{"POD_NAMESPACE": "bar"},
				},
			}
			parsed, err := parseConfigOverrides(tt.overrides)
			if err == nil {
				err = applyConfigOverrides(meshConfig, parsed)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected an error containing %q, but got: %v", tt.wantErr, err)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, meshConfig.YAML(), tt.want)
			// the result must still be a valid mesh config
			assert.NoError(t, protomarshal.ApplyJSONStrict(meshConfig.JSON(), &meshconfig.MeshConfig{}))
		})
	}
}

// TestWorkloadEntryConfigureInvalidOverrides verifies that invalid overrides are rejected
// instead of being written to the generated files.
func TestWorkloadEntryConfigureInvalidOverrides(t *testing.T) {
	testdir := t.TempDir()
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unknown mesh config field",
			args:    []string{"--overrideConfig", "defaultConfig.binarypath=/usr/bin/envoy"},
			wantErr: `unknown field "binarypath"`,
		},
		{
			name:    "malformed config override",
			args:    []string{"--overrideConfig", "defaultConfig.binaryPath"},
			wantErr: "expected the <path>=<value> format",
		},
		{
			name:    "malformed env override",
			args:    []string{"--overrideEnv", "CA_ADDR"},
			wantErr: "expected the <name>=<value> format",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{
				"entry", "configure",
				"-f", "testdata/vmconfig/ipv4/workloadgroup.yaml",
				"--internalIP", "10.10.10.10",
				"--clusterID", constants.DefaultClusterName,
				"--revision", "rev-1",
				"-o", testdir,
			}, tt.args...)
			_, err := runTestCmd(t, func(client kube.CLIClient) {
				client.Kube().CoreV1().ConfigMaps("istio-system").Create(context.Background(), &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: "istio-system", Name: "istio-rev-1"},
					Data: map[string]string{
						"mesh": string(util.ReadFile(t, "testdata/vmconfig/ipv4/meshconfig.yaml")),
					},
				}, metav1.CreateOptions{})
			}, "rev-1", args)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected an error containing %q, but got: %v", tt.wantErr, err)
			}
		})
	}
}
