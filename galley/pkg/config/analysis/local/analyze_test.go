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
	"io/ioutil"
	"os"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/mesh"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
	"istio.io/istio/galley/pkg/config/util/kubeyaml"
	"istio.io/istio/galley/pkg/testing/mock"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collection"
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
	g := NewGomegaWithT(t)

	cancel := make(chan struct{})

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankCombinedAnalyzer, "", "", nil, false, timeout)
	_, err := sa.Analyze(cancel)
	g.Expect(err).To(Not(BeNil()))
}

func TestAnalyzersRun(t *testing.T) {
	g := NewGomegaWithT(t)

	cancel := make(chan struct{})

	r := createTestResource(t, "ns", "resource", "v1")
	m := msg.NewInternalError(r, "msg")
	a := &testAnalyzer{
		fn: func(ctx analysis.Context) {
			ctx.Exists(basicmeta.K8SCollection1.Name(), resource.NewFullName("", ""))
			ctx.Report(basicmeta.K8SCollection1.Name(), m)
		},
	}

	var collectionAccessed collection.Name
	cr := func(col collection.Name) {
		collectionAccessed = col
	}

	sa := NewSourceAnalyzer(schema.MustGet(), analysis.Combine("a", a), "", "", cr, false, timeout)
	err := sa.AddReaderKubeSource(nil)
	g.Expect(err).To(BeNil())

	result, err := sa.Analyze(cancel)
	g.Expect(err).To(BeNil())
	g.Expect(result.Messages).To(ConsistOf(m))
	g.Expect(collectionAccessed).To(Equal(basicmeta.K8SCollection1.Name()))
	g.Expect(result.ExecutedAnalyzers).To(ConsistOf(a.Metadata().Name))
}

func TestFilterOutputByNamespace(t *testing.T) {
	g := NewGomegaWithT(t)

	cancel := make(chan struct{})

	r1 := createTestResource(t, "ns1", "resource", "v1")
	r2 := createTestResource(t, "ns2", "resource", "v1")
	msg1 := msg.NewInternalError(r1, "msg")
	msg2 := msg.NewInternalError(r2, "msg")
	a := &testAnalyzer{
		fn: func(ctx analysis.Context) {
			ctx.Report(basicmeta.K8SCollection1.Name(), msg1)
			ctx.Report(basicmeta.K8SCollection1.Name(), msg2)
		},
	}

	sa := NewSourceAnalyzer(schema.MustGet(), analysis.Combine("a", a), "ns1", "", nil, false, timeout)
	err := sa.AddReaderKubeSource(nil)
	g.Expect(err).To(BeNil())

	result, err := sa.Analyze(cancel)
	g.Expect(err).To(BeNil())
	g.Expect(result.Messages).To(ConsistOf(msg1))
}

func TestAddRunningKubeSource(t *testing.T) {
	g := NewGomegaWithT(t)

	mk := mock.NewKube()

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankCombinedAnalyzer, "", "", nil, false, timeout)

	sa.AddRunningKubeSource(mk)
	g.Expect(*sa.meshCfg).To(Equal(*mesh.DefaultMeshConfig())) // Base default meshcfg
	g.Expect(sa.meshNetworks.Networks).To(HaveLen(0))
	g.Expect(sa.sources).To(HaveLen(1))
	g.Expect(sa.sources[0].src).To(BeAssignableToTypeOf(&apiserver.Source{})) // Resources via api server
}

func TestAddRunningKubeSourceWithIstioMeshConfigMap(t *testing.T) {
	g := NewGomegaWithT(t)

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

	mk := mock.NewKube()
	client, err := mk.KubeClient()
	if err != nil {
		t.Fatalf("Error getting client for mock kube: %v", err)
	}
	if _, err := client.CoreV1().ConfigMaps(istioNamespace.String()).Create(context.TODO(), cfg, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Error creating mesh config configmap: %v", err)
	}

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankCombinedAnalyzer, "", istioNamespace, nil, false, timeout)

	sa.AddRunningKubeSource(mk)
	g.Expect(sa.meshCfg.RootNamespace).To(Equal(testRootNamespace))
	g.Expect(sa.meshNetworks.Networks).To(HaveLen(2))
	g.Expect(sa.sources).To(HaveLen(1))
	g.Expect(sa.sources[0].src).To(BeAssignableToTypeOf(&apiserver.Source{})) // Resources via api server
}

func TestAddReaderKubeSource(t *testing.T) {
	g := NewGomegaWithT(t)

	sa := NewSourceAnalyzer(basicmeta.MustGet(), blankCombinedAnalyzer, "", "", nil, false, timeout)

	tmpfile := tempFileFromString(t, data.YamlN1I1V1)
	defer os.Remove(tmpfile.Name())

	err := sa.AddReaderKubeSource([]ReaderSource{{Reader: tmpfile}})
	g.Expect(err).To(BeNil())
	g.Expect(*sa.meshCfg).To(Equal(*mesh.DefaultMeshConfig())) // Base default meshcfg
	g.Expect(sa.sources).To(HaveLen(1))
	g.Expect(sa.sources[0].src).To(BeAssignableToTypeOf(&inmemory.KubeSource{})) // Resources via files

	// Note that a blank file for mesh cfg is equivalent to specifying all the defaults
	testRootNamespace := "testNamespace"
	tmpMeshFile := tempFileFromString(t, fmt.Sprintf("rootNamespace: %s", testRootNamespace))
	defer func() { _ = os.Remove(tmpMeshFile.Name()) }()

	err = sa.AddFileKubeMeshConfig(tmpMeshFile.Name())
	g.Expect(err).To(BeNil())
	g.Expect(sa.meshCfg.RootNamespace).To(Equal(testRootNamespace)) // Should be mesh config from the file now
}

func TestAddReaderKubeSourceSkipsBadEntries(t *testing.T) {
	g := NewGomegaWithT(t)

	sa := NewSourceAnalyzer(basicmeta.MustGet(), blankCombinedAnalyzer, "", "", nil, false, timeout)

	tmpfile := tempFileFromString(t, kubeyaml.JoinString(data.YamlN1I1V1, "bogus resource entry\n"))
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	err := sa.AddReaderKubeSource([]ReaderSource{{Reader: tmpfile}})
	g.Expect(err).To(Not(BeNil()))
	g.Expect(sa.sources).To(HaveLen(1))
}

func TestDefaultResourcesRespectsMeshConfig(t *testing.T) {
	g := NewGomegaWithT(t)

	sa := NewSourceAnalyzer(basicmeta.MustGet(), blankCombinedAnalyzer, "", "", nil, false, timeout)

	// With ingress off, we shouldn't generate any default resources
	ingressOffMeshCfg := tempFileFromString(t, "ingressControllerMode: 'OFF'")
	defer func() { _ = os.Remove(ingressOffMeshCfg.Name()) }()

	err := sa.AddFileKubeMeshConfig(ingressOffMeshCfg.Name())
	g.Expect(err).To(BeNil())
	sa.AddDefaultResources()
	g.Expect(sa.sources).To(BeEmpty())

	// With ingress on, though, we should.
	ingressStrictMeshCfg := tempFileFromString(t, "ingressControllerMode: 'STRICT'")
	defer func() { _ = os.Remove(ingressStrictMeshCfg.Name()) }()

	err = sa.AddFileKubeMeshConfig(ingressStrictMeshCfg.Name())
	g.Expect(err).To(BeNil())
	sa.AddDefaultResources()
	g.Expect(sa.sources).To(HaveLen(1))
}

func TestResourceFiltering(t *testing.T) {
	g := NewGomegaWithT(t)

	// Set up mock apiServer so we can peek at the options it gets started with
	prevApiserverNew := apiserverNew
	defer func() { apiserverNew = prevApiserverNew }()
	var recordedOptions apiserver.Options
	apiserverNew = func(o apiserver.Options) *apiserver.Source {
		recordedOptions = o
		return nil
	}

	usedCollection := k8smeta.K8SCoreV1Services
	a := &testAnalyzer{
		fn:     func(_ analysis.Context) {},
		inputs: []collection.Name{usedCollection.Name()},
	}
	mk := mock.NewKube()

	sa := NewSourceAnalyzer(schema.MustGet(), analysis.Combine("a", a), "", "", nil, true, timeout)
	sa.AddRunningKubeSource(mk)

	// All but the used collection should be disabled
	for _, r := range recordedOptions.Schemas.All() {
		if r.Name() == usedCollection.Name() {
			g.Expect(r.IsDisabled()).To(BeFalse(), fmt.Sprintf("%s should not be disabled", r.Name()))
		} else {
			g.Expect(r.IsDisabled()).To(BeTrue(), fmt.Sprintf("%s should be disabled", r.Name()))
		}
	}
}

func tempFileFromString(t *testing.T, content string) *os.File {
	t.Helper()
	tmpfile, err := ioutil.TempFile("", "")
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
