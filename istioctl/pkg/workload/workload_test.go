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

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
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
	testCases := map[string]map[string]string{
		"ipv4": {
			"internalIP": "10.10.10.10",
			"ingressIP":  "10.10.10.11",
		},
		"ipv6": {
			"internalIP": "fd00:10:96::1",
			"ingressIP":  "fd00:10:96::2",
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

			cmdWithClusterID := []string{
				"entry", "configure",
				"-f", path.Join("testdata/vmconfig", dir.Name(), "workloadgroup.yaml"),
				"--internalIP", testCases[dir.Name()]["internalIP"],
				"--ingressIP", testCases[dir.Name()]["ingressIP"],
				"--clusterID", constants.DefaultClusterName,
				"--revision", "rev-1",
				"-o", testdir,
			}
			if _, err := runTestCmd(t, createClientFunc, "rev-1", cmdWithClusterID); err != nil {
				t.Fatal(err)
			}

			cmdNoClusterID := []string{
				"entry", "configure",
				"-f", path.Join("testdata/vmconfig", dir.Name(), "workloadgroup.yaml"),
				"--internalIP", testCases[dir.Name()]["internalIP"],
				"--revision", "rev-1",
				"-o", testdir,
			}
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

func TestSplitEqual(t *testing.T) {
	tests := []struct {
		arg       string
		wantKey   string
		wantValue string
	}{
		{arg: "key=value", wantKey: "key", wantValue: "value"},
		{arg: "key==value", wantKey: "key", wantValue: "=value"},
		{arg: "key=", wantKey: "key", wantValue: ""},
		{arg: "key", wantKey: "key", wantValue: ""},
		{arg: "", wantKey: "", wantValue: ""},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			gotKey, gotValue := splitEqual(tt.arg)
			if gotKey != tt.wantKey {
				t.Errorf("splitEqual(%v) got = %v, want %v", tt.arg, gotKey, tt.wantKey)
			}
			if gotValue != tt.wantValue {
				t.Errorf("splitEqual(%v) got1 = %v, want %v", tt.arg, gotValue, tt.wantValue)
			}
		})
	}
}
