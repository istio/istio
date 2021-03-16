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

package main

import (
	"github.com/spf13/pflag"
	"istio.io/istio/prow/asm/tester/interface/app"
	"istio.io/istio/prow/asm/tester/interface/types"
	"istio.io/istio/prow/asm/tester/pkg/pipeline"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func main() {
	app.Main("asm pipeline tester", newTester)
}

func newTester(opts types.Options) (types.BasePipelineTester, *pflag.FlagSet) {
	apt := &pipeline.ASMPipelineTester{}
	return apt, resource.BindFlags(&apt.Settings)
}
