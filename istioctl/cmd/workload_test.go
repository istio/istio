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

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/kube"
)

var fakeCACert = []byte("fake-CA-cert")

var (
	defaultYAML = `apiVersion: networking.istio.io/v1alpha3
kind: WorkloadGroup
metadata:
  name: foo
  namespace: bar
spec:
  metadata: {}
  template:
    serviceAccount: default
`

	customYAML = `apiVersion: networking.istio.io/v1alpha3
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
`
)

func TestWorkloadGroupCreate(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args - missing service name and namespace",
			args:              strings.Split("experimental workload group create", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a workload name\n",
		},
		{
			description:       "Invalid command args - missing service name",
			args:              strings.Split("experimental workload group create --namespace bar", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a workload name\n",
		},
		{
			description:       "Invalid command args - missing service namespace",
			args:              strings.Split("experimental workload group create --name foo", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a workload namespace\n",
		},
		{
			description:       "valid case - minimal flags, infer defaults",
			args:              strings.Split("experimental workload group create --name foo --namespace bar", " "),
			expectedException: false,
			expectedOutput:    defaultYAML,
		},
		{
			description: "valid case - create full workload group",
			args: strings.Split("experimental workload group create --name foo --namespace bar --labels app=foo,bar=baz "+
				" --annotations annotation=foobar --ports grpc=3550,http=8080 --serviceAccount test", " "),
			expectedException: false,
			expectedOutput:    customYAML,
		},
		{
			description: "valid case - create full workload group with shortnames",
			args: strings.Split("experimental workload group create --name foo -n bar -l app=foo,bar=baz -p grpc=3550,http=8080"+
				" -a annotation=foobar --serviceAccount test", " "),
			expectedException: false,
			expectedOutput:    customYAML,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyAddToMeshOutput(t, c)
		})
	}
}

func TestWorkloadEntryConfigureInvalidArgs(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args - missing valid input spec",
			args:              strings.Split("experimental workload entry configure --name foo -o temp --clusterID cid", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a WorkloadGroup artifact file or the name and namespace of an existing WorkloadGroup\n",
		},
		{
			description:       "Invalid command args - missing valid input spec",
			args:              strings.Split("experimental workload entry configure -n bar -o temp --clusterID cid", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a WorkloadGroup artifact file or the name and namespace of an existing WorkloadGroup\n",
		},
		{
			description:       "Invalid command args - valid filename input but missing output filename",
			args:              strings.Split("experimental workload entry configure -f file --clusterID cid", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting an output directory\n",
		},
		{
			description:       "Invalid command args - valid kubectl input but missing output filename",
			args:              strings.Split("experimental workload entry configure --name foo -n bar --clusterID cid", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting an output directory\n",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyAddToMeshOutput(t, c)
		})
	}
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
	for _, dir := range files {
		if !dir.IsDir() {
			continue
		}
		t.Run(dir.Name(), func(t *testing.T) {
			testdir := path.Join("testdata/vmconfig", dir.Name())
			kubeClientWithRevision = func(_, _, _ string) (kube.CLIClient, error) {
				return &kube.MockClient{
					RevisionValue: "rev-1",
					Interface: fake.NewSimpleClientset(
						&v1.ServiceAccount{
							ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "vm-serviceaccount"},
							Secrets:    []v1.ObjectReference{{Name: "test"}},
						},
						&v1.ConfigMap{
							ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "istio-ca-root-cert"},
							Data:       map[string]string{"root-cert.pem": string(fakeCACert)},
						},
						&v1.ConfigMap{
							ObjectMeta: metav1.ObjectMeta{Namespace: "istio-system", Name: "istio-rev-1"},
							Data: map[string]string{
								"mesh": string(util.ReadFile(t, path.Join(testdir, "meshconfig.yaml"))),
							},
						},
						&v1.Secret{
							ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "test"},
							Data: map[string][]byte{
								"token": {},
							},
						},
					),
				}, nil
			}

			cmdWithClusterID := []string{
				"x", "workload", "entry", "configure",
				"-f", path.Join("testdata/vmconfig", dir.Name(), "workloadgroup.yaml"),
				"--internalIP", "10.10.10.10",
				"--clusterID", "Kubernetes",
				"-o", testdir,
			}
			if _, err := runTestCmd(t, cmdWithClusterID); err != nil {
				t.Fatal(err)
			}

			cmdNoClusterID := []string{
				"x", "workload", "entry", "configure",
				"-f", path.Join("testdata/vmconfig", dir.Name(), "workloadgroup.yaml"),
				"--internalIP", "10.10.10.10",
				"-o", testdir,
			}
			if output, err := runTestCmd(t, cmdNoClusterID); err != nil {
				if !strings.Contains(output, noClusterID) {
					t.Fatal(err)
				}
			}

			checkFiles := map[string]bool{
				// outputs to check
				"mesh.yaml": true, "istio-token": true, "hosts": true, "root-cert.pem": true, "cluster.env": true,
				// inputs that we allow to exist, if other files seep in unexpectedly we fail the test
				".gitignore": false, "meshconfig.yaml": false, "workloadgroup.yaml": false,
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

	kubeClientWithRevision = func(_, _, _ string) (kube.CLIClient, error) {
		return &kube.MockClient{
			Interface: fake.NewSimpleClientset(
				&v1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "vm-serviceaccount"},
					Secrets:    []v1.ObjectReference{{Name: "test"}},
				},
				&v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "istio-ca-root-cert"},
					Data:       map[string]string{"root-cert.pem": string(fakeCACert)},
				},
				&v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: "istio-system", Name: "istio"},
					Data: map[string]string{
						"mesh": "defaultConfig: {}",
					},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Namespace: "bar", Name: "test"},
					Data: map[string][]byte{
						"token": {},
					},
				},
			),
		}, nil
	}

	cmdWithClusterID := []string{
		"x", "workload", "entry", "configure",
		"-f", path.Join(testdir, "workloadgroup.yaml"),
		"--internalIP", "10.10.10.10",
		"--clusterID", "Kubernetes",
		"-o", testdir,
	}
	if output, err := runTestCmd(t, cmdWithClusterID); err != nil {
		t.Logf("output: %v", output)
		t.Fatal(err)
	}

	cmdNoClusterID := []string{
		"x", "workload", "entry", "configure",
		"-f", path.Join(testdir, "workloadgroup.yaml"),
		"--internalIP", "10.10.10.10",
		"-o", testdir,
	}
	if output, err := runTestCmd(t, cmdNoClusterID); err != nil {
		if !strings.Contains(output, noClusterID) {
			t.Fatal(err)
		}
	}

	checkFiles := map[string]bool{
		// outputs to check
		"mesh.yaml": true, "istio-token": true, "hosts": true, "root-cert.pem": true, "cluster.env": true,
		// inputs that we allow to exist, if other files seep in unexpectedly we fail the test
		".gitignore": false, "workloadgroup.yaml": false,
	}

	checkOutputFiles(t, testdir, checkFiles)
}

func runTestCmd(t *testing.T, args []string) (string, error) {
	t.Helper()
	// TODO there is already probably something else that does this
	var out bytes.Buffer
	rootCmd := GetRootCmd(args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	err := rootCmd.Execute()
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
