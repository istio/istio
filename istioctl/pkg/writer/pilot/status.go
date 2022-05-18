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
	"strings"
	"text/tabwriter"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	xdsstatus "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"

	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	xdsresource "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/pkg/log"
)

// StatusWriter enables printing of sync status using multiple []byte Istiod responses
type StatusWriter struct {
	Writer io.Writer
}

type writerStatus struct {
	pilot string
	xds.SyncStatus
}

// XdsStatusWriter enables printing of sync status using multiple xdsapi.DiscoveryResponse Istiod responses
type XdsStatusWriter struct {
	Writer                 io.Writer
	InternalDebugAllIstiod bool
}

type xdsWriterStatus struct {
	proxyID              string
	clusterID            string
	istiodID             string
	istiodVersion        string
	clusterStatus        string
	listenerStatus       string
	routeStatus          string
	endpointStatus       string
	extensionconfigStaus string
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
	w := new(tabwriter.Writer).Init(s.Writer, 0, 9, 5, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tCLUSTER\tCDS\tLDS\tEDS\tRDS\tECDS\tISTIOD\tVERSION")
	fullStatus := make([]*writerStatus, 0, len(statuses))
	for pilot, status := range statuses {
		var ss []*writerStatus
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
		if fullStatus[i].ClusterID != fullStatus[j].ClusterID {
			return fullStatus[i].ClusterID < fullStatus[j].ClusterID
		}
		return fullStatus[i].ProxyID < fullStatus[j].ProxyID
	})
	return w, fullStatus, nil
}

func statusPrintln(w io.Writer, status *writerStatus) error {
	clusterSynced := xdsStatus(status.ClusterSent, status.ClusterAcked)
	listenerSynced := xdsStatus(status.ListenerSent, status.ListenerAcked)
	routeSynced := xdsStatus(status.RouteSent, status.RouteAcked)
	endpointSynced := xdsStatus(status.EndpointSent, status.EndpointAcked)
	extensionconfigSynced := xdsStatus(status.ExtensionConfigSent, status.ExtensionConfigAcked)
	version := status.IstioVersion
	if version == "" {
		// If we can't find an Istio version (talking to a 1.1 pilot), fallback to the proxy version
		// This is misleading, as the proxy version isn't always the same as the Istio version,
		// but it is better than not providing any information.
		version = status.ProxyVersion + "*"
	}
	_, _ = fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\n",
		status.ProxyID, status.ClusterID,
		clusterSynced, listenerSynced, endpointSynced, routeSynced, extensionconfigSynced,
		status.pilot, version)
	return nil
}

func xdsStatus(sent, acked string) string {
	if sent == "" {
		return "NOT SENT"
	}
	if sent == acked {
		return "SYNCED"
	}
	// acked will be empty string when there is never Acknowledged
	if acked == "" {
		return "STALE (Never Acknowledged)"
	}
	// Since the Nonce changes to uuid, so there is no more any time diff info
	return "STALE"
}

// PrintAll takes a slice of Istiod syncz responses and outputs them using a tabwriter
func (s *XdsStatusWriter) PrintAll(statuses map[string]*xdsapi.DiscoveryResponse) error {
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

func (s *XdsStatusWriter) setupStatusPrint(drs map[string]*xdsapi.DiscoveryResponse) (*tabwriter.Writer, []*xdsWriterStatus, error) {
	// Gather the statuses before printing so they may be sorted
	var fullStatus []*xdsWriterStatus
	mappedResp := map[string]string{}
	var w *tabwriter.Writer
	for id, dr := range drs {
		for _, resource := range dr.Resources {
			switch resource.TypeUrl {
			case "type.googleapis.com/envoy.service.status.v3.ClientConfig":
				clientConfig := xdsstatus.ClientConfig{}
				err := resource.UnmarshalTo(&clientConfig)
				if err != nil {
					return nil, nil, fmt.Errorf("could not unmarshal ClientConfig: %w", err)
				}
				cds, lds, eds, rds, ecds := getSyncStatus(&clientConfig)
				cp := multixds.CpInfo(dr)
				meta, err := model.ParseMetadata(clientConfig.GetNode().GetMetadata())
				if err != nil {
					return nil, nil, fmt.Errorf("could not parse node metadata: %w", err)
				}
				fullStatus = append(fullStatus, &xdsWriterStatus{
					proxyID:              clientConfig.GetNode().GetId(),
					clusterID:            meta.ClusterID.String(),
					istiodID:             cp.ID,
					istiodVersion:        cp.Info.Version,
					clusterStatus:        cds,
					listenerStatus:       lds,
					routeStatus:          rds,
					endpointStatus:       eds,
					extensionconfigStaus: ecds,
				})
				if len(fullStatus) == 0 {
					return nil, nil, fmt.Errorf("no proxies found (checked %d istiods)", len(drs))
				}

				w = new(tabwriter.Writer).Init(s.Writer, 0, 8, 5, ' ', 0)
				_, _ = fmt.Fprintln(w, "NAME\tCLUSTER\tCDS\tLDS\tEDS\tRDS\tECDS\tISTIOD\tVERSION")

				sort.Slice(fullStatus, func(i, j int) bool {
					return fullStatus[i].proxyID < fullStatus[j].proxyID
				})
			default:
				for _, resource := range dr.Resources {
					if s.InternalDebugAllIstiod {
						mappedResp[id] = string(resource.Value) + "\n"
					} else {
						_, _ = s.Writer.Write(resource.Value)
						_, _ = s.Writer.Write([]byte("\n"))
					}
				}
				fullStatus = nil
			}
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
		status.extensionconfigStaus,
		status.istiodID, status.istiodVersion)
	return err
}

func getSyncStatus(clientConfig *xdsstatus.ClientConfig) (cds, lds, eds, rds, ecds string) {
	configs := handleAndGetXdsConfigs(clientConfig)
	for _, config := range configs {
		cfgType := config.GetTypeUrl()
		switch cfgType {
		case xdsresource.ListenerType:
			lds = config.GetConfigStatus().String()
		case xdsresource.ClusterType:
			cds = config.GetConfigStatus().String()
		case xdsresource.RouteType:
			rds = config.GetConfigStatus().String()
		case xdsresource.EndpointType:
			eds = config.GetConfigStatus().String()
		case xdsresource.ExtensionConfigurationType:
			ecds = config.GetConfigStatus().String()
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
