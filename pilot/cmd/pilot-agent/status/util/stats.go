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

package util

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/pkg/env"
)

const (
	statCdsRejected    = "cluster_manager.cds.update_rejected"
	statsCdsSuccess    = "cluster_manager.cds.update_success"
	statLdsRejected    = "listener_manager.lds.update_rejected"
	statLdsSuccess     = "listener_manager.lds.update_success"
	statServerState    = "server.state"
	statWorkersStarted = "listener_manager.workers_started"
	readyStatsRegex    = "^(server\\.state|listener_manager\\.workers_started)"
	updateStatsRegex   = "^(cluster_manager\\.cds|listener_manager\\.lds)\\.(update_success|update_rejected)$"
)

var (
	readinessTimeout = env.RegisterDurationVar("ENVOY_READINESS_CHECK_TIMEOUT", time.Second*5, "").Get()
)

type stat struct {
	name  string
	value *uint64
	found bool
}

// Stats contains values of interest from a poll of Envoy stats.
type Stats struct {
	// Update Stats.
	CDSUpdatesSuccess   uint64
	CDSUpdatesRejection uint64
	LDSUpdatesSuccess   uint64
	LDSUpdatesRejection uint64
	// Server State of Envoy.
	ServerState    uint64
	WorkersStarted uint64
}

// String representation of the Stats.
func (s *Stats) String() string {
	return fmt.Sprintf("cds updates: %d successful, %d rejected; lds updates: %d successful, %d rejected",
		s.CDSUpdatesSuccess,
		s.CDSUpdatesRejection,
		s.LDSUpdatesSuccess,
		s.LDSUpdatesRejection)
}

// GetReadinessStats returns the current Envoy state by checking the "server.state" stat.
func GetReadinessStats(localHostAddr string, adminPort uint16) (*uint64, bool, error) {
	// If the localHostAddr was not set, we use 'localhost' to void emppty host in URL.
	if localHostAddr == "" {
		localHostAddr = "localhost"
	}

	readinessURL := fmt.Sprintf("http://%s:%d/stats?usedonly&filter=%s", localHostAddr, adminPort, readyStatsRegex)
	stats, err := doHTTPGetWithTimeout(readinessURL, readinessTimeout)
	if err != nil {
		return nil, false, err
	}
	if !strings.Contains(stats.String(), "server.state") {
		return nil, false, fmt.Errorf("server.state is not yet updated: %s", stats.String())
	}

	if !strings.Contains(stats.String(), "listener_manager.workers_started") {
		return nil, false, fmt.Errorf("listener_manager.workers_started is not yet updated: %s", stats.String())
	}

	s := &Stats{}
	allStats := []*stat{
		{name: statServerState, value: &s.ServerState},
		{name: statWorkersStarted, value: &s.WorkersStarted},
	}
	if err := parseStats(stats, allStats); err != nil {
		return nil, false, err
	}

	return &s.ServerState, s.WorkersStarted == 1, nil
}

// GetUpdateStatusStats returns the version stats for CDS and LDS.
func GetUpdateStatusStats(localHostAddr string, adminPort uint16) (*Stats, error) {
	// If the localHostAddr was not set, we use 'localhost' to void emppty host in URL.
	if localHostAddr == "" {
		localHostAddr = "localhost"
	}

	stats, err := doHTTPGet(fmt.Sprintf("http://%s:%d/stats?usedonly&filter=%s", localHostAddr, adminPort, updateStatsRegex))
	if err != nil {
		return nil, err
	}

	s := &Stats{}
	allStats := []*stat{
		{name: statsCdsSuccess, value: &s.CDSUpdatesSuccess},
		{name: statCdsRejected, value: &s.CDSUpdatesRejection},
		{name: statLdsSuccess, value: &s.LDSUpdatesSuccess},
		{name: statLdsRejected, value: &s.LDSUpdatesRejection},
	}
	if err := parseStats(stats, allStats); err != nil {
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
