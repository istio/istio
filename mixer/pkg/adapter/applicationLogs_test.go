// Copyright 2017 Istio Authors
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

package adapter

import (
	"bytes"
	"testing"
)

func TestSeverity(t *testing.T) {
	i := 0
	for s := range severityMap {
		str := s.String()
		sev, ok := SeverityByName(str)

		if !ok {
			t.Error("Got !ok, expecting true")
		}

		if sev != s {
			t.Errorf("%d: Got %v, expected %v", i, sev, s)
		}
		i++
	}

	sev := Severity(-1)
	str := sev.String()
	if str != "-1" {
		t.Errorf("Got %s, expecting -1", str)
	}

	severity, ok := SeverityByName("FOOBAR")
	if severity != Default {
		t.Errorf("Got %d, expecting Default", severity)
	}

	if ok {
		t.Error("Got ok==true, expecting false")
	}

	sev = Emergency
	buf, err := sev.MarshalJSON()
	if err != nil {
		t.Errorf("Got %v, expecting success", err)
	}

	if !bytes.Contains(buf, []byte("EMERGENCY")) {
		t.Errorf("Expecting buffer to contain EMERGENCY, got %v", string(buf))
	}
}
