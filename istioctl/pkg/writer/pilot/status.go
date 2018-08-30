// Copyright 2018 Istio Authors.
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

package pilot

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

// StatusWriter enables printing of sync status using multiple []byte Pilot responses
type StatusWriter struct {
	Writer io.Writer
}

type writerStatus struct {
	pilot string
	v2.SyncStatus
}

// PrintAll takes a slice of Pilot syncz responses and outputs them using a tabwriter
func (s *StatusWriter) PrintAll(statuses map[string][]byte) error {
	w, fullStatus, err := s.setupStatusPrint(statuses)
	if err != nil {
		return err
	}
	for _, status := range fullStatus {
		if err := statusPrintln(w, status); err != nil {
			return err
		}
	}
	return w.Flush()
}

// PrintSingle takes a slice of Pilot syncz responses and outputs them using a tabwriter filtering for a specific pod
func (s *StatusWriter) PrintSingle(statuses map[string][]byte, proxyName string) error {
	w, fullStatus, err := s.setupStatusPrint(statuses)
	if err != nil {
		return err
	}
	for _, status := range fullStatus {
		if strings.Contains(status.ProxyID, proxyName) {
			if err := statusPrintln(w, status); err != nil {
				return err
			}
		}
	}
	return w.Flush()
}

func (s *StatusWriter) setupStatusPrint(statuses map[string][]byte) (*tabwriter.Writer, []*writerStatus, error) {
	w := new(tabwriter.Writer).Init(s.Writer, 0, 8, 5, ' ', 0)
	fmt.Fprintln(w, "PROXY\tCDS\tLDS\tEDS\tRDS\tPILOT\tVERSION")
	fullStatus := []*writerStatus{}
	for pilot, status := range statuses {
		ss := []*writerStatus{}
		err := json.Unmarshal(status, &ss)
		if err != nil {
			return nil, nil, err
		}
		for _, s := range ss {
			s.pilot = pilot
		}
		fullStatus = append(fullStatus, ss...)
	}
	sort.Slice(fullStatus, func(i, j int) bool {
		return fullStatus[i].ProxyID < fullStatus[j].ProxyID
	})
	return w, fullStatus, nil
}

func statusPrintln(w io.Writer, status *writerStatus) error {
	clusterSynced := xdsStatus(status.ClusterSent, status.ClusterAcked)
	listenerSynced := xdsStatus(status.ListenerSent, status.ListenerAcked)
	routeSynced := xdsStatus(status.RouteSent, status.RouteAcked)
	endpointSynced := xdsStatus(status.EndpointSent, status.EndpointAcked)
	fmt.Fprintf(w, "%v\t%v\t%v\t%v (%v%%)\t%v\t%v\t%v\n",
		status.ProxyID, clusterSynced, listenerSynced, endpointSynced, status.EndpointPercent, routeSynced, status.pilot, status.ProxyVersion)
	return nil
}

func xdsStatus(sent, acked string) string {
	if sent == "" {
		return "NOT SENT"
	}
	if sent == acked {
		return "SYNCED"
	}
	return "STALE"
}
