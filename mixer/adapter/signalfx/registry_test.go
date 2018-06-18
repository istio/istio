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

package signalfx

import (
	"testing"
	"time"
)

func setTime(r *registry, t time.Time) {
	r.currentTime = func() time.Time { return t }
}

func advanceTime(r *registry, minutes int64) {
	setTime(r, time.Unix(r.currentTime().Unix()+minutes*60, 0))
}

func TestExpiration(t *testing.T) {
	r := newRegistry(5 * time.Minute)

	setTime(r, time.Unix(100, 0))

	r.RegisterOrGetCumulative("test", map[string]string{"a": "1"})
	r.RegisterOrGetCumulative("test", map[string]string{"a": "2"})
	r.RegisterOrGetCumulative("test", map[string]string{"a": "3"})
	r.RegisterOrGetCumulative("test", map[string]string{"a": "4"})

	r.Datapoints()
	if len(r.Datapoints()) != 4 {
		t.Fatalf("Expected four datapoints")
	}

	advanceTime(r, 4)

	r.Datapoints()
	if len(r.Datapoints()) != 4 {
		t.Fatalf("Expected four datapoints")
	}

	r.RegisterOrGetCumulative("test", map[string]string{"a": "4"})
	r.RegisterOrGetCumulative("test", map[string]string{"a": "5"})

	r.Datapoints()
	if len(r.Datapoints()) != 5 {
		t.Fatalf("Expected five datapoints")
	}

	advanceTime(r, 2)

	r.Datapoints()
	dps := r.Datapoints()
	if len(dps) != 2 {
		t.Fatalf("Expected first three counters to be expired")
	}

	for _, dp := range dps {
		if dp.Dimensions["a"] == "1" || dp.Dimensions["a"] == "2" || dp.Dimensions["a"] == "3" {
			t.Fatalf("Wrong counter was expired")
		}
	}
}
