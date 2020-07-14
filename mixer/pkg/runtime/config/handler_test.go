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

package config

import (
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/runtime/testing/data"
)

var globalCfg = data.JoinConfigs(data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1)

func TestBasic(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil)
	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]
	h, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err != nil {
		t.Fatal(err)
	}

	if h == nil {
		t.Fail()
	}
}

func TestAdapterSupportsAdditionalTemplates(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", SupportedTemplates: []string{"additional-template"}})

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	h, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err != nil {
		t.Fatal()
	}

	if h == nil {
		t.Fatal()
	}
}

func TestInferError(t *testing.T) {
	templates := data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", ErrorAtInferType: true})
	attributes := data.BuildAdapters(nil)

	_, err := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)
	if err == nil || !strings.Contains(err.Error(), "infer type error") {
		t.Fatalf("want error with 'infer type error'; got %v", err)
	}
}

func TestNilBuilder(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", NilBuilder: true})

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestBuilderDoesNotSupportTemplate(t *testing.T) {
	templates := data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", BuilderDoesNotSupportTemplate: true})
	attributes := data.BuildAdapters(nil)

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestHandlerDoesNotSupportTemplate(t *testing.T) {
	templates := data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", HandlerDoesNotSupportTemplate: true})
	attributes := data.BuildAdapters(nil)

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestHandlerDoesNotSupportTemplate_ErrorDuringClose(t *testing.T) {
	// Trigger premature close due to handler not supporting the template.
	templates := data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", HandlerDoesNotSupportTemplate: true})
	attributes := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", ErrorAtHandlerClose: true})

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestHandlerDoesNotSupportTemplate_PanicDuringClose(t *testing.T) {
	// Trigger premature close due to handler not supporting the template.
	templates := data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", HandlerDoesNotSupportTemplate: true})
	attributes := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", PanicAtHandlerClose: true})

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestPanicAtSetAdapterConfig(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", PanicAtSetAdapterConfig: true})

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestFailedValidation(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", ErrorAtValidate: true})

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestPanicAtValidation(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", PanicAtValidate: true})

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestFailedBuild(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", ErrorAtBuild: true})

	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]

	_, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err == nil {
		t.Fatal()
	}
}

func TestElided(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil)
	s, _ := GetSnapshotForTest(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.InstancesStatic[data.FqnI1]
	h1 := s.HandlersStatic[data.FqnACheck1]
	h, err := BuildHandler(h1, []*InstanceStatic{i1}, nil, s.Templates)
	if err != nil {
		t.Fatal(err)
	}

	if h == nil {
		t.Fail()
	}
}
