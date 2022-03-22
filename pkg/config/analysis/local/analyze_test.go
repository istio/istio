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
package local

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/kube"
)

type testAnalyzer struct {
	fn     func(analysis.Context)
	inputs collection.Names
}

var blankTestAnalyzer = &testAnalyzer{
	fn:     func(_ analysis.Context) {},
	inputs: []collection.Name{},
}

var (
	// YamlN1I1V1 is a testing resource in Yaml form
	YamlN1I1V1 = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind1
metadata:
  namespace: n1
  name: i1
spec:
  n1_i1: v1
`
	blankCombinedAnalyzer = analysis.Combine("testCombined", blankTestAnalyzer)
	timeout               = 1 * time.Second
)

// Metadata implements Analyzer
func (a *testAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:   "testAnalyzer",
		Inputs: a.inputs,
	}
}

// Analyze implements Analyzer
func (a *testAnalyzer) Analyze(ctx analysis.Context) {
	a.fn(ctx)
}

func TestAbortWithNoSources(t *testing.T) {
	g := NewWithT(t)

	cancel := make(chan struct{})

	sa := NewSourceAnalyzer(blankCombinedAnalyzer, "", "", nil, false, timeout)
	_, err := sa.Analyze(cancel)
	g.Expect(err).To(Not(BeNil()))
}

func TestAnalyzersRun(t *testing.T) {
	g := NewWithT(t)

	cancel := make(chan struct{})

	r := createTestResource(t, "ns", "resource", "v1")
	m := msg.NewInternalError(r, "msg")
	a := &testAnalyzer{
		fn: func(ctx analysis.Context) {
			ctx.Exists(K8SCollection1.Name(), resource.NewFullName("", ""))
			ctx.Report(K8SCollection1.Name(), m)
		},
	}

	var collectionAccessed collection.Name
	cr := func(col collection.Name) {
		collectionAccessed = col
	}

	sa := NewSourceAnalyzer(analysis.Combine("a", a), "", "", cr, false, timeout)
	err := sa.AddReaderKubeSource(nil)
	g.Expect(err).To(BeNil())

	result, err := sa.Analyze(cancel)
	g.Expect(err).To(BeNil())
	g.Expect(result.Messages).To(ConsistOf(m))
	g.Expect(collectionAccessed).To(Equal(K8SCollection1.Name()))
	g.Expect(result.ExecutedAnalyzers).To(ConsistOf(a.Metadata().Name))
}

func TestFilterOutputByNamespace(t *testing.T) {
	g := NewWithT(t)

	cancel := make(chan struct{})

	r1 := createTestResource(t, "ns1", "resource", "v1")
	r2 := createTestResource(t, "ns2", "resource", "v1")
	msg1 := msg.NewInternalError(r1, "msg")
	msg2 := msg.NewInternalError(r2, "msg")
	a := &testAnalyzer{
		fn: func(ctx analysis.Context) {
			ctx.Report(K8SCollection1.Name(), msg1)
			ctx.Report(K8SCollection1.Name(), msg2)
		},
	}

	sa := NewSourceAnalyzer(analysis.Combine("a", a), "ns1", "", nil, false, timeout)
	err := sa.AddReaderKubeSource(nil)
	g.Expect(err).To(BeNil())

	result, err := sa.Analyze(cancel)
	g.Expect(err).To(BeNil())
	g.Expect(result.Messages).To(ConsistOf(msg1))
}

func TestAddInMemorySource(t *testing.T) {
	g := NewWithT(t)

	sa := NewSourceAnalyzer(blankCombinedAnalyzer, "", "", nil, false, timeout)

	src := model.NewFakeStore()
	sa.AddSource(dfCache{ConfigStore: src})
	g.Expect(sa.meshCfg).To(Equal(mesh.DefaultMeshConfig())) // Base default meshcfg
	g.Expect(sa.meshNetworks.Networks).To(HaveLen(0))
	g.Expect(sa.stores).To(HaveLen(1))
}

func TestAddRunningKubeSource(t *testing.T) {
	g := NewWithT(t)

	mk := kube.NewFakeClient()

	sa := NewSourceAnalyzer(blankCombinedAnalyzer, "", "", nil, false, timeout)

	sa.AddRunningKubeSource(mk)
	g.Expect(sa.meshCfg).To(Equal(mesh.DefaultMeshConfig())) // Base default meshcfg
	g.Expect(sa.meshNetworks.Networks).To(HaveLen(0))
	g.Expect(sa.stores).To(HaveLen(1))
}

func TestAddRunningKubeSourceWithIstioMeshConfigMap(t *testing.T) {
	g := NewWithT(t)

	istioNamespace := resource.Namespace("istio-system")

	testRootNamespace := "testNamespace"

	cfg := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: meshConfigMapName,
		},
		Data: map[string]string{
			meshConfigMapKey:   fmt.Sprintf("rootNamespace: %s", testRootNamespace),
			meshNetworksMapKey: `networks: {"n1": {}, "n2": {}}`,
		},
	}

	mk := kube.NewFakeClient()
	if _, err := mk.Kube().CoreV1().ConfigMaps(istioNamespace.String()).Create(context.TODO(), cfg, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Error creating mesh config configmap: %v", err)
	}

	sa := NewSourceAnalyzer(blankCombinedAnalyzer, "", istioNamespace, nil, false, timeout)

	sa.AddRunningKubeSource(mk)
	g.Expect(sa.meshCfg.RootNamespace).To(Equal(testRootNamespace))
	g.Expect(sa.meshNetworks.Networks).To(HaveLen(2))
	g.Expect(sa.stores).To(HaveLen(1))
}

func TestAddReaderKubeSource(t *testing.T) {
	g := NewWithT(t)

	sa := NewSourceAnalyzer(blankCombinedAnalyzer, "", "", nil, false, timeout)

	tmpfile := tempFileFromString(t, YamlN1I1V1)
	defer os.Remove(tmpfile.Name())

	err := sa.AddReaderKubeSource([]ReaderSource{{Reader: tmpfile}})
	g.Expect(err).To(BeNil())
	g.Expect(sa.meshCfg).To(Equal(mesh.DefaultMeshConfig())) // Base default meshcfg
	g.Expect(sa.stores).To(HaveLen(0))

	// Note that a blank file for mesh cfg is equivalent to specifying all the defaults
	testRootNamespace := "testNamespace"
	tmpMeshFile := tempFileFromString(t, fmt.Sprintf("rootNamespace: %s", testRootNamespace))
	defer func() { _ = os.Remove(tmpMeshFile.Name()) }()

	err = sa.AddFileKubeMeshConfig(tmpMeshFile.Name())
	g.Expect(err).To(BeNil())
	g.Expect(sa.meshCfg.RootNamespace).To(Equal(testRootNamespace)) // Should be mesh config from the file now
}

func TestAddReaderKubeSourceSkipsBadEntries(t *testing.T) {
	g := NewWithT(t)

	sa := NewSourceAnalyzer(blankCombinedAnalyzer, "", "", nil, false, timeout)

	tmpfile := tempFileFromString(t, JoinString(YamlN1I1V1, "bogus resource entry\n"))
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	err := sa.AddReaderKubeSource([]ReaderSource{{Reader: tmpfile}})
	g.Expect(err).To(Not(BeNil()))
}

const (
	yamlSeparator = "---\n"
)

// JoinString joins the given yaml parts into a single multipart document.
func JoinString(parts ...string) string {
	var st strings.Builder

	var lastIsNewLine bool
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		if st.Len() != 0 {
			if !lastIsNewLine {
				_, _ = st.WriteString("\n")
			}
			st.WriteString(yamlSeparator)
		}
		_, _ = st.WriteString(p)
		lastIsNewLine = p[len(p)-1] == '\n'
	}

	return st.String()
}

func TestDefaultResourcesRespectsMeshConfig(t *testing.T) {
	g := NewWithT(t)

	sa := NewSourceAnalyzer(blankCombinedAnalyzer, "", "", nil, false, timeout)

	// With ingress off, we shouldn't generate any default resources
	ingressOffMeshCfg := tempFileFromString(t, "ingressControllerMode: 'OFF'")
	defer func() { _ = os.Remove(ingressOffMeshCfg.Name()) }()

	err := sa.AddFileKubeMeshConfig(ingressOffMeshCfg.Name())
	g.Expect(err).To(BeNil())
	sa.AddDefaultResources()
	g.Expect(sa.stores).To(BeEmpty())

	// With ingress on, though, we should.
	ingressStrictMeshCfg := tempFileFromString(t, "ingressControllerMode: 'STRICT'")
	defer func() { _ = os.Remove(ingressStrictMeshCfg.Name()) }()

	err = sa.AddFileKubeMeshConfig(ingressStrictMeshCfg.Name())
	g.Expect(err).To(BeNil())
	sa.AddDefaultResources()
	g.Expect(sa.stores).To(HaveLen(0))
}

func tempFileFromString(t *testing.T, content string) *os.File {
	t.Helper()
	tmpfile, err := os.CreateTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = tmpfile.WriteString(content)
	if err != nil {
		t.Fatal(err)
	}
	err = tmpfile.Sync()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tmpfile.Seek(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return tmpfile
}
