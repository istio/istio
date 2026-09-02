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

package analyze

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util/testutil"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
)

func TestErrorOnIssuesFound(t *testing.T) {
	g := NewWithT(t)

	msgs := []diag.Message{
		diag.NewMessage(
			diag.NewMessageType(diag.Error, "B1", "Template: %q"),
			nil,
			"",
		),
		diag.NewMessage(
			diag.NewMessageType(diag.Warning, "A1", "Template: %q"),
			nil,
			"",
		),
	}

	err := errorIfMessagesExceedThreshold(msgs)

	g.Expect(err).To(BeIdenticalTo(AnalyzerFoundIssuesError{}))
}

func TestNoErrorIfMessageLevelsBelowThreshold(t *testing.T) {
	g := NewWithT(t)

	msgs := []diag.Message{
		diag.NewMessage(
			diag.NewMessageType(diag.Info, "B1", "Template: %q"),
			nil,
			"",
		),
		diag.NewMessage(
			diag.NewMessageType(diag.Warning, "A1", "Template: %q"),
			nil,
			"",
		),
	}

	err := errorIfMessagesExceedThreshold(msgs)

	g.Expect(err).To(BeNil())
}

func TestSkipPodsInFiles(t *testing.T) {
	c := testutil.TestCase{
		Args: strings.Split(
			"-A --use-kube=false --failure-threshold ERROR testdata/analyze-file/public-gateway.yaml",
			" "),
		WantException: false,
	}
	analyze := Analyze(cli.NewFakeContext(nil))
	testutil.VerifyOutput(t, analyze, c)
}

func TestGetClientsRejectsUnsafeMultiClusterSecret(t *testing.T) {
	g := NewWithT(t)

	maliciousKubeconfig := []byte(`
apiVersion: v1
kind: Config
clusters:
- name: remote
  cluster:
    server: https://remote.example.com
contexts:
- name: remote-context
  context:
    cluster: remote
    user: attacker
current-context: remote-context
users:
- name: attacker
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: /bin/sh
      args: ["-c", "touch /tmp/pwned-by-istioctl"]
      interactiveMode: Never
`)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "remote-cluster-secret",
			Namespace: "istio-system",
			Labels: map[string]string{
				multicluster.MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{
			"remote-cluster": maliciousKubeconfig,
		},
	}

	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
		IstioNamespace: "istio-system",
		Objects:        []runtime.Object{secret},
	})

	_, err := getClients(ctx)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("exec is not allowed"))
}

func TestGetClientsAllowsLegitimateTokenSecret(t *testing.T) {
	g := NewWithT(t)

	oldRevision := revisionSpecified
	revisionSpecified = "canary"
	t.Cleanup(func() { revisionSpecified = oldRevision })

	legitKubeconfig := []byte(`
apiVersion: v1
kind: Config
clusters:
- name: remote
  cluster:
    server: https://127.0.0.1:6443
    insecure-skip-tls-verify: true
contexts:
- name: remote-context
  context:
    cluster: remote
    user: remote-sa
current-context: remote-context
users:
- name: remote-sa
  user:
    token: some-bearer-token
`)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "remote-cluster-secret",
			Namespace: "istio-system",
			Labels: map[string]string{
				multicluster.MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{
			"remote-cluster": legitKubeconfig,
		},
	}

	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
		IstioNamespace: "istio-system",
		Objects:        []runtime.Object{secret},
	})

	clients, err := getClients(ctx)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(clients).To(HaveLen(2)) // local fake client + the sanitized remote one

	remote := clients[1]
	g.Expect(remote.remote).To(BeTrue())
	g.Expect(remote.client.ClusterID()).To(Equal(cluster.ID("remote")))

	cliClient, ok := remote.client.(kube.CLIClient)
	g.Expect(ok).To(BeTrue())
	g.Expect(cliClient.Revision()).To(Equal("canary"))
}

func TestRunSpecificAnalyzer(t *testing.T) {
	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
		IstioNamespace: "istio-system",
	})

	cases := []struct {
		caseName string
		testutil.TestCase
	}{
		{
			caseName: "failed-with-all-analyzers",
			TestCase: testutil.TestCase{
				Args: strings.Split(
					"--use-kube=false testdata/analyze-file/specific-analyzer.yaml",
					" "),
				WantException: true,
			},
		},
		{
			caseName: "failed-with-specific-analyzer",
			TestCase: testutil.TestCase{
				Args: strings.Split(
					"--use-kube=false --analyzer schema.ValidationAnalyzer.Gateway testdata/analyze-file/specific-analyzer.yaml",
					" "),
				WantException: true,
			},
		},
		{
			caseName: "passed-with-specific-analyzer",
			TestCase: testutil.TestCase{
				Args: strings.Split(
					"--use-kube=false --analyzer gateway.ConflictingGatewayAnalyzer testdata/analyze-file/specific-analyzer.yaml",
					" "),
				WantException: false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.caseName, func(t *testing.T) {
			analyze := Analyze(ctx)
			testutil.VerifyOutput(t, analyze, tc.TestCase)
		})
	}
}
