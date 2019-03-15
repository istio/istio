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

package qualification

import (
	"istio.io/istio/pkg/test/framework2/components/istio"
	"istio.io/istio/pkg/test/framework2/core"
	"istio.io/istio/pkg/test/framework2/runtime"
	"testing"

	"istio.io/istio/pkg/test/framework2"
)

var (
	ist istio.Instance
)

func TestMain(m *testing.M) {
	framework2.RunSuite("qualification", m, setup)
}

func setup(s *runtime.SuiteContext) error {
	switch s.Environment().EnvironmentName() {
	case core.Kube:
		i, err := istio.New(s, nil)
		if err != nil {
			return err
		}
		ist = i
	case core.Native:
	}

	return nil
}
