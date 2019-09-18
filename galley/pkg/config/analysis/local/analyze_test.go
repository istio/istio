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
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
	"istio.io/istio/galley/pkg/testing/mock"
)

type testAnalyzer struct {
	fn              func(analysis.Context)
	collectionsUsed collection.Names
}

var blankTestAnalyzer = &testAnalyzer{
	fn:              func(_ analysis.Context) {},
	collectionsUsed: []collection.Name{},
}

// Metadata implements Analyzer
func (a *testAnalyzer) Metadata() analysis.Metadata {
	return analysis.NewMetadata("testAnalyzer", a.collectionsUsed)
}

// Analyze implements Analyzer
func (a *testAnalyzer) Analyze(ctx analysis.Context) {
	a.fn(ctx)
}

func TestAbortWithNoSources(t *testing.T) {
	g := NewGomegaWithT(t)

	cancel := make(chan struct{})

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankTestAnalyzer)
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
			ctx.Report(collection.NewName("collection"), msg)
		},
	}

	sa := NewSourceAnalyzer(metadata.MustGet(), a)
	sa.AddFileKubeSource([]string{}, "")

	msgs, err := sa.Analyze(cancel)
	g.Expect(err).To(BeNil())
	g.Expect(msgs).To(ConsistOf(msg))
}

func TestAddRunningKubeSource(t *testing.T) {
	g := NewGomegaWithT(t)

	mk := mock.NewKube()

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankTestAnalyzer)

	sa.AddRunningKubeSource(mk)
	g.Expect(sa.sources).To(HaveLen(1))
}

func TestAddFileKubeSource(t *testing.T) {
	g := NewGomegaWithT(t)

	sa := NewSourceAnalyzer(k8smeta.MustGet(), blankTestAnalyzer)

	tmpfile, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	_, err = tmpfile.WriteString(data.YamlN1I1V1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	sa.AddFileKubeSource([]string{tmpfile.Name()}, "")
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
	dummyAnalyzer := &testAnalyzer{
		fn:              func(_ analysis.Context) {},
		collectionsUsed: []collection.Name{usedCollection},
	}
	mk := mock.NewKube()

	sa := NewSourceAnalyzer(metadata.MustGet(), dummyAnalyzer)
	sa.AddRunningKubeSource(mk)

	// All but the used collection should be disabled
	for _, r := range recordedOptions.Resources {
		if r.Collection.Name == k8smeta.K8SCoreV1Services {
			g.Expect(r.Disabled).To(BeFalse(), fmt.Sprintf("%s should not be disabled", r.Collection.Name))
		} else {
			g.Expect(r.Disabled).To(BeTrue(), fmt.Sprintf("%s should be disabled", r.Collection.Name))
		}
	}
}
