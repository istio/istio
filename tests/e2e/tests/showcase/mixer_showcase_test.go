//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package showcase

import (
	"testing"
	"istio.io/istio/pkg/test"
)

func TestMixer_Report(t *testing.T) {
	test.Requires(t, mixer.RemoteSpyAdapter)

	m := mixer.Get()

	//m.Configure(mixer.StandardConfig)
	m.Configure(testConfig)

	a := m.GetSpyAdapter()

	err := m.Report(map[string]interface{}{
		"target.name": "somesrvcname",
	})

	found := a.Expect([]interface{}{
		&reportTmpl.Instance{
			Name:       "reportInstance.samplereport.istio-system",
			Value:      int64(2),
			Dimensions: map[string]interface{}{"source": "mysrc", "target_ip": "somesrvcname"},
		},
	})

	if !found {
		t.Fatalf("failed")
	}
}


var testConfig = 	`
apiVersion: "config.istio.io/v1alpha2"
kind: fakeHandler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system

---

apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: reportInstance
  namespace: istio-system
spec:
  value: "2"
  dimensions:
    source: source.name | "mysrc"
    target_ip: target.name | "mytarget"

---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  selector: match(target.name, "*")
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - reportInstance.samplereport

`)