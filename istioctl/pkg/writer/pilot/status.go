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

package pilot

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	xdsstatus "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"k8s.io/apimachinery/pkg/util/duration"

	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/pilot/pkg/model"
	xdsresource "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/log"
)

// XdsStatusWriter enables printing of sync status using multiple xdsapi.DiscoveryResponse Istiod responses
type XdsStatusWriter struct {
	Writer                 io.Writer
	Namespace              string
	InternalDebugAllIstiod bool
}

type xdsWriterStatus struct {
	proxyID       string
	clusterID     string
	istiodID      string
	istiodVersion string
	typeStatus    map[string]string // typeURL -> status
}

const ignoredStatus = "IGNORED"

// PrintAll takes a slice of Istiod syncz responses and outputs them using a tabwriter
func (s *XdsStatusWriter) PrintAll(statuses map[string]*discovery.DiscoveryResponse) error {
	w, fullStatus, allTypes, err := s.setupStatusPrint(statuses)
	if err != nil {
		return err
	}
	// Print header dynamically
	headers := []string{"NAME", "CLUSTER"}
	headers = append(headers, allTypes...)
	headers = append(headers, "ISTIOD", "VERSION")
	fmt.Fprintln(w, joinWithTabs(headers))
	for _, status := range fullStatus {
		if err := xdsStatusPrintlnDynamic(w, status, allTypes); err != nil {
			return err
		}
	}
	if w != nil {
		return w.Flush()
	}
	return nil
}

// joinWithTabs joins a string slice with tabs
func joinWithTabs(fields []string) string {
	return fmt.Sprintf("%s", stringJoin(fields, "\t"))
}

func stringJoin(a []string, sep string) string {
	if len(a) == 0 {
		return ""
	}
	res := a[0]
	for _, s := range a[1:] {
		res += sep + s
	}
	return res
}

func (s *XdsStatusWriter) setupStatusPrint(drs map[string]*discovery.DiscoveryResponse) (*tabwriter.Writer, []*xdsWriterStatus, []string, error) {
	var fullStatus []*xdsWriterStatus
	allTypesSet := map[string]struct{}{}
	w := new(tabwriter.Writer).Init(s.Writer, 0, 8, 5, ' ', 0)
	for _, dr := range drs {
		for _, resource := range dr.Resources {
			clientConfig := xdsstatus.ClientConfig{}
			if err := resource.UnmarshalTo(&clientConfig); err != nil {
				return nil, nil, nil, fmt.Errorf("could not unmarshal ClientConfig: %w", err)
			}
			meta, err := model.ParseMetadata(clientConfig.GetNode().GetMetadata())
			if err != nil {
				return nil, nil, nil, fmt.Errorf("could not parse node metadata: %w", err)
			}
			if s.Namespace != "" && meta.Namespace != s.Namespace {
				continue
			}
			configs := handleAndGetXdsConfigs(&clientConfig)
			typeStatus := map[string]string{}
			for _, config := range configs {
				typeURL := config.GetTypeUrl()
				allTypesSet[typeURL] = struct{}{}
				typeStatus[typeURL] = formatStatus(config)
			}
			// For types not present, mark as IGNORED
			fullStatus = append(fullStatus, &xdsWriterStatus{
				proxyID:       clientConfig.GetNode().GetId(),
				clusterID:     meta.ClusterID.String(),
				istiodID:      multixds.CpInfo(dr).ID,
				istiodVersion: meta.IstioVersion,
				typeStatus:    typeStatus,
			})
		}
	}
	if len(fullStatus) == 0 {
		return nil, nil, nil, fmt.Errorf("no proxies found (checked %d istiods)", len(drs))
	}
	// Sort types for consistent output
	allTypes := make([]string, 0, len(allTypesSet))
	for t := range allTypesSet {
		allTypes = append(allTypes, t)
	}
	sort.Strings(allTypes)
	// Sort proxies by proxyID
	sort.Slice(fullStatus, func(i, j int) bool {
		return fullStatus[i].proxyID < fullStatus[j].proxyID
	})
	return w, fullStatus, allTypes, nil
}

// Print a row for each proxy, filling in status for each type
func xdsStatusPrintlnDynamic(w io.Writer, status *xdsWriterStatus, allTypes []string) error {
	fields := []string{status.proxyID, status.clusterID}
	for _, t := range allTypes {
		val, ok := status.typeStatus[t]
		if !ok {
			val = ignoredStatus
		}
		fields = append(fields, val)
	}
	fields = append(fields, status.istiodID, status.istiodVersion)
	_, err := fmt.Fprintln(w, joinWithTabs(fields))
	return err
}

func formatStatus(s *xdsstatus.ClientConfig_GenericXdsConfig) string {
	switch s.GetConfigStatus() {
	case xdsstatus.ConfigStatus_UNKNOWN:
		return ignoredStatus
	case xdsstatus.ConfigStatus_NOT_SENT:
		return "NOT SENT"
	default:
		status := s.GetConfigStatus().String()
		if s.LastUpdated != nil {
			status += " (" + duration.HumanDuration(time.Since(s.LastUpdated.AsTime())) + ")"
		}
		return status
	}
}

func getSyncStatus(clientConfig *xdsstatus.ClientConfig) (cds, lds, eds, rds, ecds string) {
	// If type is not found at all, it is considered ignored
	lds = ignoredStatus
	cds = ignoredStatus
	rds = ignoredStatus
	eds = ignoredStatus
	ecds = ignoredStatus
	configs := handleAndGetXdsConfigs(clientConfig)
	for _, config := range configs {
		cfgType := config.GetTypeUrl()
		switch cfgType {
		case xdsresource.ListenerType:
			lds = formatStatus(config)
		case xdsresource.ClusterType:
			cds = formatStatus(config)
		case xdsresource.RouteType:
			rds = formatStatus(config)
		case xdsresource.EndpointType:
			eds = formatStatus(config)
		case xdsresource.ExtensionConfigurationType:
			ecds = formatStatus(config)
		default:
			log.Infof("GenericXdsConfig unexpected type %s\n", xdsresource.GetShortType(cfgType))
		}
	}
	return
}

func handleAndGetXdsConfigs(clientConfig *xdsstatus.ClientConfig) []*xdsstatus.ClientConfig_GenericXdsConfig {
	configs := make([]*xdsstatus.ClientConfig_GenericXdsConfig, 0)
	if clientConfig.GetGenericXdsConfigs() != nil {
		configs = clientConfig.GetGenericXdsConfigs()
		return configs
	}

	// FIXME: currently removing the deprecated code below may result in functions not working
	// if there is a mismatch of versions between istiod and istioctl
	// nolint: staticcheck
	for _, config := range clientConfig.GetXdsConfig() {
		var typeURL string
		switch config.PerXdsConfig.(type) {
		case *xdsstatus.PerXdsConfig_ListenerConfig:
			typeURL = xdsresource.ListenerType
		case *xdsstatus.PerXdsConfig_ClusterConfig:
			typeURL = xdsresource.ClusterType
		case *xdsstatus.PerXdsConfig_RouteConfig:
			typeURL = xdsresource.RouteType
		case *xdsstatus.PerXdsConfig_EndpointConfig:
			typeURL = xdsresource.EndpointType
		}

		if typeURL != "" {
			configs = append(configs, &xdsstatus.ClientConfig_GenericXdsConfig{
				TypeUrl:      typeURL,
				ConfigStatus: config.Status,
			})
		}
	}

	return configs
}
