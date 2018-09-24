// Copyright 2018 Istio Authors
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
	"io/ioutil"
	"testing"

	"os"
	"strings"

	newrelic "istio.io/istio/mixer/adapter/newrelic/pkg"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

func TestReport(t *testing.T) {
	adptCrBytes, err := ioutil.ReadFile("config/newrelic.yaml")
	if err != nil {
		t.Fatalf("could not read file: %v", err)
	}

	operatorCfgBytes, err := ioutil.ReadFile("sample_operator_cfg.yaml")
	if err != nil {
		t.Fatalf("could not read file: %v", err)
	}
	operatorCfg := string(operatorCfgBytes)
	shutdown := make(chan error, 1)

	var outFile *os.File
	outFile, err = os.OpenFile("out.txt", os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if removeErr := os.Remove(outFile.Name()); removeErr != nil {
			t.Logf("Could not remove temporary file %s: %v", outFile.Name(), removeErr)
		}
	}()

	adapter_integration.RunTest(
		t,
		nil,
		adapter_integration.Scenario{
			Setup: func() (ctx interface{}, err error) {
				pServer, err := newrelic.NewGrpcAdapter("49951")
				if err != nil {
					return nil, err
				}
				go func() {
					pServer.Run(shutdown)
					_ = <-shutdown
				}()
				return pServer, nil
			},
			Teardown: func(ctx interface{}) {
				s := ctx.(newrelic.Server)
				s.Close()
			},
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.size": 555},
				},
			},
			GetState: nil,
			GetConfig: func(ctx interface{}) ([]string, error) {
				s := ctx.(newrelic.Server)
				return []string{
					// CRs for built-in templates (metric is what we need for this test)
					// are automatically added by the integration test framework.
					string(adptCrBytes),
					strings.Replace(operatorCfg, "{ADDRESS}", s.Addr(), 1),
				}, nil
			},
			Want: `
	   {
		"AdapterState": null,
		"Returns": [
		 {
		  "Check": {
		   "Status": {},
		   "ValidDuration": 0,
		   "ValidUseCount": 0
		  },
		  "Quota": null,
		  "Error": null
		 }
		]
	   }`,
		},
	)
}

func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Replace(s, "\t", "", -1)
	s = strings.Replace(s, "\n", "", -1)
	s = strings.Replace(s, " ", "", -1)
	return s
}
