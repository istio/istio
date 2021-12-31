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
	"bytes"
	"flag"
	"testing"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/framework/config"
)

const (
	testCfg    = "testdata/config.yaml"
	goldenFile = "testdata/golden.yaml"
)

func TestGenerate(t *testing.T) {
	if !config.Parsed() {
		config.Parse()
	}
	flag.VisitAll(func(f *flag.Flag) {
		// these change often, don't want to regen golden
		switch f.Name {
		case "istio.test.tag":
			_ = f.Value.Set("testtag")
		case "istio.test.hub":
			_ = f.Value.Set("testhub")
		case "istio.test.pullpolicy":
			_ = f.Value.Set("IfNotPresent")
		}
	})
	out := bytes.NewBuffer([]byte{})
	generate(testCfg, "", false, out)
	util.CompareContent(t, out.Bytes(), goldenFile)
}
