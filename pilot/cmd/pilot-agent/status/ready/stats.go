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

package ready

import (
	"bytes"
	"fmt"

	"istio.io/istio/pilot/cmd/pilot-agent/status/stats"
)

const (
	statCdsUpdatesSuccess   = "cluster_manager.cds.update_success"
	statCdsUpdatesRejection = "cluster_manager.cds.update_rejected"
	statLdsUpdatesSuccess   = "listener_manager.lds.update_success"
	statLdsUpdatesRejection = "listener_manager.lds.update_rejected"
)

// Stats contains values of interest from a poll of Envoy stats.
type Stats struct {
	CDSUpdatesSuccess   uint64
	CDSUpdatesRejection uint64
	LDSUpdatesSuccess   uint64
	LDSUpdatesRejection uint64
}

// String representation of the Stats.
func (s *Stats) String() string {
	return fmt.Sprintf("cds updates: %d successful, %d rejected; lds updates: %d successful, %d rejected",
		s.CDSUpdatesSuccess,
		s.CDSUpdatesRejection,
		s.LDSUpdatesSuccess,
		s.LDSUpdatesRejection)
}

// ParseStats from an Envoy stats doc.
func ParseStats(input *bytes.Buffer) (*Stats, error) {
	// Parse the Envoy stats.
	s := &Stats{}

	p := &stats.ProcessorChain{}
	p.Match(exact(statCdsUpdatesSuccess)).Then(set(&s.CDSUpdatesSuccess))
	p.Match(exact(statCdsUpdatesRejection)).Then(set(&s.CDSUpdatesRejection))
	p.Match(exact(statLdsUpdatesSuccess)).Then(set(&s.LDSUpdatesSuccess))
	p.Match(exact(statLdsUpdatesRejection)).Then(set(&s.LDSUpdatesRejection))

	if err := p.ProcessInput(input); err != nil {
		return nil, err
	}

	return s, nil
}

func exact(name string) stats.MatchFunc {
	return stats.Exact(name)
}

func set(val *uint64) stats.ProcessFunc {
	return stats.Set(val)
}
