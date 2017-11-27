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

package perf

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"
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
	StableOrder bool `json:"stableOrder,omitempty"`
}

// MarshalJSON marshal the load as JSON.
func (l *Load) MarshalJSON() ([]byte, error) {
	tmp := struct {
		Iterations  int               `json:"iterations,omitempty"`
		StableOrder bool              `json:"stableOrder,omitempty"`
		Requests    []json.RawMessage `json:"requests,omitempty"`
	}{
		Iterations:  l.Multiplier,
		StableOrder: l.StableOrder,
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
	var tmp struct {
		Iterations  int               `json:"iterations,omitempty"`
		StableOrder bool              `json:"stableOrder,omitempty"`
		Requests    []json.RawMessage `json:"requests,omitempty"`
	}
	err := json.Unmarshal(bytes, &tmp)
	if err != nil {
		return err
	}

	l.Multiplier = tmp.Iterations
	l.StableOrder = tmp.StableOrder

	l.Requests = make([]Request, len(tmp.Requests))
	for i, raw := range tmp.Requests {
		var rtmp struct {
			Type string `json:"type"`
		}
		err := json.Unmarshal(raw, &rtmp)
		if err != nil {
			return err
		}

		switch rtmp.Type {
		case "basicReport":
			var r BasicReport
			err = json.Unmarshal(raw, &r)
			if err != nil {
				return err
			}
			l.Requests[i] = &r

		case "basicCheck":
			var r BasicCheck
			err = json.Unmarshal(raw, &r)
			if err != nil {
				return err
			}
			l.Requests[i] = &r

		default:
			return fmt.Errorf("unrecognized request type: '%v'", rtmp.Type)
		}
	}

	return nil
}

func (l *Load) createRequestProtos(c Config) []interface{} {
	initialSet := []interface{}{}
	for _, r := range l.Requests {
		initialSet = append(initialSet, r.createRequestProtos(c)...)
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
		source := rand.NewSource(time.Now().UnixNano())
		random := rand.New(source)
		for j := len(requests) - 1; j > 0; j-- {
			k := random.Intn(j + 1)
			requests[j], requests[k] = requests[k], requests[j]
		}
	}

	return requests
}
