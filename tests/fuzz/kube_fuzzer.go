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

// nolint: golint
package fuzz

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func setupKubeSource() *inmemory.KubeSource {
	s := inmemory.NewKubeSource(basicmeta.MustGet().KubeCollections())
	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)
	return s
}

func applyFuzzedContent(f *fuzz.ConsumeFuzzer, s *inmemory.KubeSource) error {
	name, err := f.GetString()
	if err != nil {
		return err
	}
	yamlText, err := f.GetString()
	if err != nil {
		return err
	}
	_ = s.ApplyContent(name, yamlText)
	return nil
}

func removeFuzzedContent(f *fuzz.ConsumeFuzzer, s *inmemory.KubeSource) error {
	name, err := f.GetString()
	if err != nil {
		return err
	}
	s.RemoveContent(name)
	return nil
}

func FuzzInmemoryKube(data []byte) int {
	f := fuzz.NewConsumer(data)
	s := setupKubeSource()
	s.Start()
	defer s.Stop()

	numberOfIterations, err := f.GetInt()
	if err != nil {
		return 0
	}
	totalIters := numberOfIterations % 30
	for i := 0; i < totalIters; i++ {
		action, err := f.GetInt()
		if err != nil {
			return 0
		}
		if action%1 == 0 {
			err = applyFuzzedContent(f, s)
			if err != nil {
				return 0
			}
		} else if action%2 == 0 {
			err = removeFuzzedContent(f, s)
			if err != nil {
				return 0
			}
		} else if action%3 == 0 {
			_ = s.ContentNames()
		}
	}
	return 1
}
