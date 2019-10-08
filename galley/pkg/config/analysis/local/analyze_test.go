// Copyright 2019 Istio Authors
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
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
	"istio.io/istio/galley/pkg/testing/mock"
)

type testAnalyzer struct {
	fn     func(analysis.Context)
	inputs collection.Names
}

var blankTestAnalyzer = &testAnalyzer{
	fn:     func(_ analysis.Context) {},
	inputs: []collection.Name{},
}

var blankCombinedAnalyzer = analysis.Combine("testCombined", blankTestAnalyzer)

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

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankCombinedAnalyzer, nil, false)
	_, err := sa.Analyze(cancel)
	g.Expect(err).To(Not(BeNil()))
}

func TestAnalyzersRun(t *testing.T) {
	g := NewGomegaWithT(t)

	cancel := make(chan struct{})

	r := createTestResource("resource", "v1")
	msg := msg.NewInternalError(r, "msg")
	a := &testAnalyzer{
		fn: func(ctx analysis.Context) {
			ctx.Exists(data.Collection1, resource.NewName("", ""))
			ctx.Report(data.Collection1, msg)
		},
	}

	var collectionAccessed collection.Name
	cr := func(col collection.Name) {
		collectionAccessed = col
	}

	sa := NewSourceAnalyzer(metadata.MustGet(), analysis.Combine("a", a), cr, false)
	err := sa.AddFileKubeSource([]string{}, "")
	g.Expect(err).To(BeNil())

	msgs, err := sa.Analyze(cancel)
	g.Expect(err).To(BeNil())
	g.Expect(msgs).To(ConsistOf(msg))
	g.Expect(collectionAccessed).To(Equal(data.Collection1))
}

func TestAddRunningKubeSource(t *testing.T) {
	g := NewGomegaWithT(t)

	mk := mock.NewKube()

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankCombinedAnalyzer, nil, false)

	sa.AddRunningKubeSource(mk)
	g.Expect(sa.sources).To(HaveLen(1))
}

func TestAddFileKubeSource(t *testing.T) {
	g := NewGomegaWithT(t)

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankCombinedAnalyzer, nil, false)

	tmpfile, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()
	_, err = tmpfile.WriteString(data.YamlN1I1V1)
	if err != nil {
		t.Fatal(err)
	}
	if err = tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	err = sa.AddFileKubeSource([]string{tmpfile.Name()}, "")
	g.Expect(err).To(BeNil())
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
		inputs: []collection.Name{usedCollection},
	}
	mk := mock.NewKube()

	sa := NewSourceAnalyzer(metadata.MustGet(), analysis.Combine("a", a), nil, true)
	sa.AddRunningKubeSource(mk)

	// All but the used collection should be disabled
	for _, r := range recordedOptions.Resources {
		if r.Collection.Name == usedCollection {
			g.Expect(r.Disabled).To(BeFalse(), fmt.Sprintf("%s should not be disabled", r.Collection.Name))
		} else {
			g.Expect(r.Disabled).To(BeTrue(), fmt.Sprintf("%s should be disabled", r.Collection.Name))
		}
	}
}
