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

package conformance

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"
)

func TestHasStages_False(t *testing.T) {
	g := NewGomegaWithT(t)

	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	b, err := hasStages(d)
	g.Expect(err).To(BeNil())
	g.Expect(b).To(BeFalse())
}

func TestHasStages_True(t *testing.T) {
	g := NewGomegaWithT(t)

	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	err = os.Mkdir(path.Join(d, "stage0"), os.ModePerm)
	g.Expect(err).To(BeNil())

	b, err := hasStages(d)
	g.Expect(err).To(BeNil())
	g.Expect(b).To(BeTrue())
}

func TestHasStages_Error(t *testing.T) {
	g := NewGomegaWithT(t)

	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	_, err = hasStages(path.Join(d, "notexists"))
	g.Expect(err).NotTo(BeNil())
}

func TestParseStageName(t *testing.T) {
	var cases = []struct {
		Input string
		Ok    bool
		Idx   int
	}{
		{
			Input: "",
			Ok:    false,
			Idx:   -1,
		},
		{
			Input: "st",
			Ok:    false,
			Idx:   -1,
		},
		{
			Input: "stage",
			Ok:    false,
			Idx:   -2,
		},
		{
			Input: "stage0",
			Ok:    true,
			Idx:   0,
		},
		{
			Input: "stage21",
			Ok:    true,
			Idx:   21,
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)

			actIdx, actOk := parseStageName(c.Input)
			g.Expect(actOk).To(Equal(c.Ok))
			g.Expect(actIdx).To(Equal(c.Idx))
		})
	}
}

func TestLoadStage(t *testing.T) {
	g := NewGomegaWithT(t)

	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	err = ioutil.WriteFile(path.Join(d, InputFileName), nil, os.ModePerm)
	g.Expect(err).To(BeNil())

	s, err := loadStage(d)
	g.Expect(err).To(BeNil())

	expected := &Stage{
		Input:      "",
		MCP:        nil,
		MeshConfig: nil,
	}

	g.Expect(s).To((Equal(expected)))
}
