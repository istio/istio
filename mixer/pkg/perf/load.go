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

package perf

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"istio.io/pkg/log"
)

// Load is the load to apply on the Mixer during the perf test.
// TODO: This should become an interface, and we should be able to specify different load generation strategies.
type Load struct {

	// Requests is the set of requests to use.
	// TODO: These should all be collapsed to "Requests",
	Requests []Request `json:"requests,omitempty"`

	// Multiplier is the number of times to apply the specified load on target. If not specified, it will default to 1.
	Multiplier int `json:"multiplier,omitempty"`

	// StableOrder indicates that the requests will be executed in a stable order. If not set to true, then the
	// requests will be randomized before being sent over the wire.
	// This is here mostly for debugging.
	StableOrder bool `json:"stableOrder,omitempty"`

	// RandomSeed is the random seed to use when randomizing load. If omitted (i.e. when set to 0), a time-based seed
	// will be used.
	// This is here mostly for debugging.
	RandomSeed int64 `json:"randomSeed,omitempty"`
}

type loadSerializationState struct {
	Iterations  int               `json:"iterations,omitempty"`
	StableOrder bool              `json:"stableOrder,omitempty"`
	RandomSeed  int64             `json:"randomSeed,omitempty"`
	Requests    []json.RawMessage `json:"requests,omitempty"`
}

// MarshalJSON marshals the load as JSON.
func (l *Load) MarshalJSON() ([]byte, error) {
	tmp := loadSerializationState{
		Iterations:  l.Multiplier,
		StableOrder: l.StableOrder,
		RandomSeed:  l.RandomSeed,
		Requests:    make([]json.RawMessage, len(l.Requests)),
	}

	for i, r := range l.Requests {
		rb, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}

		rr := json.RawMessage(rb)
		tmp.Requests[i] = rr
	}

	return json.Marshal(&tmp)
}

// UnmarshalJSON unmarshals the load from JSON.
func (l *Load) UnmarshalJSON(bytes []byte) error {
	tmp := loadSerializationState{}
	if err := json.Unmarshal(bytes, &tmp); err != nil {
		return err
	}

	l.Multiplier = tmp.Iterations
	l.StableOrder = tmp.StableOrder
	l.RandomSeed = tmp.RandomSeed

	l.Requests = make([]Request, len(tmp.Requests))
	for i, raw := range tmp.Requests {
		var rtmp struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &rtmp); err != nil {
			return err
		}

		switch rtmp.Type {
		case "basicReport":
			var r BasicReport
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			l.Requests[i] = r

		case "basicCheck":
			var r BasicCheck
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			l.Requests[i] = r

		default:
			return fmt.Errorf("unrecognized request type: '%v'", rtmp.Type)
		}
	}

	return nil
}

func (l *Load) createRequestProtos() []interface{} {
	initialSet := []interface{}{}
	for _, r := range l.Requests {
		initialSet = append(initialSet, r.getRequestProto())
	}

	requests := initialSet

	if l.Multiplier > 1 {
		requests = []interface{}{}
		for j := 0; j < l.Multiplier; j++ {
			requests = append(requests, initialSet...)
		}
	}

	// Shuffle requests if StableOrder is not explicitly requested.
	if !l.StableOrder {
		seed := l.RandomSeed
		if seed == 0 {
			seed = time.Now().UnixNano()
		}
		log.Infof("Random seed for load randomization: '%v'", seed)
		// Do not sync the log here to avoid polluting perf timings.
		source := rand.NewSource(seed)
		random := rand.New(source)
		for j := len(requests) - 1; j > 0; j-- {
			k := random.Intn(j + 1)
			requests[j], requests[k] = requests[k], requests[j]
		}
	}

	return requests
}
