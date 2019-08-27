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

package util

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
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

// GetStats from Envoy.
func GetStats(localHostAddr string, adminPort uint16) (*Stats, error) {
	input, err := doHTTPGet(fmt.Sprintf("http://%s:%d/stats?usedonly", localHostAddr, adminPort))
	if err != nil {
		return nil, multierror.Prefix(err, "failed retrieving Envoy stats:")
	}

	// Parse the Envoy stats.
	s := &Stats{}
	allStats := []*stat{
		{name: statCdsUpdatesSuccess, value: &s.CDSUpdatesSuccess},
		{name: statCdsUpdatesRejection, value: &s.CDSUpdatesRejection},
		{name: statLdsUpdatesSuccess, value: &s.LDSUpdatesSuccess},
		{name: statLdsUpdatesRejection, value: &s.LDSUpdatesRejection},
	}
	if err := parseStats(input, allStats); err != nil {
		return nil, err
	}

	return s, nil
}

func parseStats(input *bytes.Buffer, stats []*stat) (err error) {
	for input.Len() > 0 {
		line, _ := input.ReadString('\n')
		for _, stat := range stats {
			if e := stat.processLine(line); e != nil {
				err = multierror.Append(err, e)
			}
		}
	}
	for _, stat := range stats {
		if !stat.found {
			*stat.value = 0
		}
	}
	return
}

type stat struct {
	name  string
	value *uint64
	found bool
}

func (s *stat) processLine(line string) error {
	if !s.found && strings.HasPrefix(line, s.name) {
		s.found = true

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			return fmt.Errorf("envoy stat %s missing separator. line:%s", s.name, line)
		}

		val, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("failed parsing Envoy stat %s (error: %s) line: %s", s.name, err.Error(), line)
		}

		*s.value = val
	}

	return nil
}
