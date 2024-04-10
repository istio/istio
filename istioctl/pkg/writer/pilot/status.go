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
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	xdsstatus "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"

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
	proxyID               string
	clusterID             string
	istiodID              string
	istiodVersion         string
	clusterStatus         string
	listenerStatus        string
	routeStatus           string
	endpointStatus        string
	extensionconfigStatus string
}

const ignoredStatus = "IGNORED"

// PrintAll takes a slice of Istiod syncz responses and outputs them using a tabwriter
func (s *XdsStatusWriter) PrintAll(statuses map[string]*discovery.DiscoveryResponse) error {
	w, fullStatus, err := s.setupStatusPrint(statuses)
	if err != nil {
		return err
	}
	for _, status := range fullStatus {
		if err := xdsStatusPrintln(w, status); err != nil {
			return err
		}
	}
	if w != nil {
		return w.Flush()
	}
	return nil
}

func (s *XdsStatusWriter) setupStatusPrint(drs map[string]*discovery.DiscoveryResponse) (*tabwriter.Writer, []*xdsWriterStatus, error) {
	// Gather the statuses before printing so they may be sorted
	var fullStatus []*xdsWriterStatus
	mappedResp := map[string]string{}
	w := new(tabwriter.Writer).Init(s.Writer, 0, 8, 5, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tCLUSTER\tCDS\tLDS\tEDS\tRDS\tECDS\tISTIOD\tVERSION")
	for _, dr := range drs {
		for _, resource := range dr.Resources {
			clientConfig := xdsstatus.ClientConfig{}
			err := resource.UnmarshalTo(&clientConfig)
			if err != nil {
				return nil, nil, fmt.Errorf("could not unmarshal ClientConfig: %w", err)
			}
			meta, err := model.ParseMetadata(clientConfig.GetNode().GetMetadata())
			if err != nil {
				return nil, nil, fmt.Errorf("could not parse node metadata: %w", err)
			}
			if s.Namespace != "" && meta.Namespace != s.Namespace {
				continue
			}
			cds, lds, eds, rds, ecds := getSyncStatus(&clientConfig)
			cp := multixds.CpInfo(dr)
			fullStatus = append(fullStatus, &xdsWriterStatus{
				proxyID:               clientConfig.GetNode().GetId(),
				clusterID:             meta.ClusterID.String(),
				istiodID:              cp.ID,
				istiodVersion:         meta.IstioVersion,
				clusterStatus:         cds,
				listenerStatus:        lds,
				routeStatus:           rds,
				endpointStatus:        eds,
				extensionconfigStatus: ecds,
			})
			if len(fullStatus) == 0 {
				return nil, nil, fmt.Errorf("no proxies found (checked %d istiods)", len(drs))
			}

			sort.Slice(fullStatus, func(i, j int) bool {
				return fullStatus[i].proxyID < fullStatus[j].proxyID
			})
		}
	}
	if len(mappedResp) > 0 {
		mresp, err := json.MarshalIndent(mappedResp, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		_, _ = s.Writer.Write(mresp)
		_, _ = s.Writer.Write([]byte("\n"))
	}

	return w, fullStatus, nil
}

func xdsStatusPrintln(w io.Writer, status *xdsWriterStatus) error {
	_, err := fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\n",
		status.proxyID, status.clusterID,
		status.clusterStatus, status.listenerStatus, status.endpointStatus, status.routeStatus,
		status.extensionconfigStatus,
		status.istiodID, status.istiodVersion)
	return err
}

func formatStatus(s *xdsstatus.ClientConfig_GenericXdsConfig) string {
	switch s.GetConfigStatus() {
	case xdsstatus.ConfigStatus_UNKNOWN:
		return ignoredStatus
	case xdsstatus.ConfigStatus_NOT_SENT:
		return "NOT SENT"
	default:
		return s.GetConfigStatus().String()
	}
}

func getSyncStatus(clientConfig *xdsstatus.ClientConfig) (cds, lds, eds, rds, ecds string) {
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
