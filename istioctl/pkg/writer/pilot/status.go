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
	"strings"
	"text/tabwriter"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	xdsstatus "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"k8s.io/apimachinery/pkg/util/duration"

	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/pilot/pkg/model"
	xdsresource "istio.io/istio/pilot/pkg/xds/v3"
)

// XdsStatusWriter enables printing of sync status using multiple xdsapi.DiscoveryResponse Istiod responses
type XdsStatusWriter struct {
	Writer                 io.Writer
	Namespace              string
	InternalDebugAllIstiod bool
	OutputFormat           string // Output format: "table" or "json"
	Verbosity              int    // Verbosity level: 0=default, 1=max
}

// xdsWriterStatus represents the status of a single proxy's XDS configuration
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
	if s.Verbosity > 0 {
		return s.printAllVerbose(statuses)
	}
	w, fullStatus, allTypes, err := s.setupStatusPrint(statuses)
	if err != nil {
		return fmt.Errorf("failed to setup status print: %w", err)
	}

	// Print header dynamically with shortened type names
	headers := []string{"NAME", "CLUSTER"}
	for _, t := range allTypes {
		headers = append(headers, xdsresource.GetShortType(t))
	}
	headers = append(headers, "ISTIOD", "VERSION")
	if _, err := fmt.Fprintln(w, joinWithTabs(headers)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Print each proxy's status
	for _, status := range fullStatus {
		if err := xdsStatusPrintlnDynamic(w, status, allTypes); err != nil {
			return fmt.Errorf("failed to print status for proxy %s: %w", status.proxyID, err)
		}
	}

	if w != nil {
		if err := w.Flush(); err != nil {
			return fmt.Errorf("failed to flush tabwriter: %w", err)
		}
	}
	return nil
}

// printAllVerbose prints all known xDS types for all proxies, even if not present in the data.
func (s *XdsStatusWriter) printAllVerbose(statuses map[string]*discovery.DiscoveryResponse) error {
	// Collect all types ever seen, plus all known xDS types (including CRDs, etc.)
	w, fullStatus, allTypes, err := s.setupStatusPrint(statuses)
	if err != nil {
		return fmt.Errorf("failed to setup status print: %w", err)
	}

	// Add all known xDS types (from pilotxds.TypeURLs)
	knownTypes := getAllKnownXdsTypes()
	typeSet := map[string]struct{}{}
	for _, t := range allTypes {
		typeSet[t] = struct{}{}
	}
	for _, t := range knownTypes {
		typeSet[t] = struct{}{}
	}
	// Rebuild allTypes with all known types, sorted
	allTypesVerbose := make([]string, 0, len(typeSet))
	for t := range typeSet {
		allTypesVerbose = append(allTypesVerbose, t)
	}
	sort.Strings(allTypesVerbose)

	// Print header
	headers := []string{"NAME", "CLUSTER"}
	for _, t := range allTypesVerbose {
		headers = append(headers, xdsresource.GetShortType(t))
	}
	headers = append(headers, "ISTIOD", "VERSION")
	if _, err := fmt.Fprintln(w, joinWithTabs(headers)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Print each proxy's status
	for _, status := range fullStatus {
		if err := xdsStatusPrintlnDynamic(w, status, allTypesVerbose); err != nil {
			return fmt.Errorf("failed to print status for proxy %s: %w", status.proxyID, err)
		}
	}

	if w != nil {
		if err := w.Flush(); err != nil {
			return fmt.Errorf("failed to flush tabwriter: %w", err)
		}
	}
	return nil
}

// getAllKnownXdsTypes returns a list of all known xDS type URLs, including core and extension types.
func getAllKnownXdsTypes() []string {
	// Core types
	types := []string{
		xdsresource.ClusterType,
		xdsresource.ListenerType,
		xdsresource.EndpointType,
		xdsresource.RouteType,
		xdsresource.ExtensionConfigurationType,
		// Add more known types here as needed
	}
	// In the future, this could be extended to include CRDs or dynamically discovered types
	return types
}

// joinWithTabs joins a string slice with tabs
func joinWithTabs(fields []string) string {
	return stringJoin(fields, "\t")
}

// stringJoin efficiently joins a string slice with a separator
func stringJoin(a []string, sep string) string {
	if len(a) == 0 {
		return ""
	}
	if len(a) == 1 {
		return a[0]
	}

	// Pre-allocate buffer with approximate size
	n := len(sep) * (len(a) - 1)
	for _, s := range a {
		n += len(s)
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(a[0])
	for _, s := range a[1:] {
		b.WriteString(sep)
		b.WriteString(s)
	}
	return b.String()
}

// setupStatusPrint processes the discovery responses and prepares them for printing
func (s *XdsStatusWriter) setupStatusPrint(drs map[string]*discovery.DiscoveryResponse) (*tabwriter.Writer, []*xdsWriterStatus, []string, error) {
	var fullStatus []*xdsWriterStatus
	allTypesSet := map[string]struct{}{}
	w := new(tabwriter.Writer).Init(s.Writer, 0, 8, 5, ' ', 0)

	// Process each discovery response
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

			// Skip if namespace filter is set and doesn't match
			if s.Namespace != "" && meta.Namespace != s.Namespace {
				continue
			}

			// Process XDS configs
			configs := handleAndGetXdsConfigs(&clientConfig)
			typeStatus := make(map[string]string, len(configs))
			for _, config := range configs {
				typeURL := config.GetTypeUrl()
				allTypesSet[typeURL] = struct{}{}
				typeStatus[typeURL] = formatStatus(config)
			}

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

	// Sort types for consistent output, preserving legacy order for core types
	coreOrder := []string{
		xdsresource.ClusterType,  // CDS
		xdsresource.ListenerType, // LDS
		xdsresource.EndpointType, // EDS
		xdsresource.RouteType,    // RDS
	}
	var allTypes []string
	added := map[string]bool{}
	for _, t := range coreOrder {
		if _, ok := allTypesSet[t]; ok {
			allTypes = append(allTypes, t)
			added[t] = true
		}
	}
	// Add any extra types in sorted order
	var extras []string
	for t := range allTypesSet {
		if !added[t] {
			extras = append(extras, t)
		}
	}
	sort.Strings(extras)
	allTypes = append(allTypes, extras...)

	// Sort proxies by proxyID
	sort.Slice(fullStatus, func(i, j int) bool {
		return fullStatus[i].proxyID < fullStatus[j].proxyID
	})

	return w, fullStatus, allTypes, nil
}

// xdsStatusPrintlnDynamic prints a single row of proxy status with dynamic type columns
func xdsStatusPrintlnDynamic(w io.Writer, status *xdsWriterStatus, allTypes []string) error {
	fields := []string{status.proxyID, status.clusterID}

	// Add status for each type, using IGNORED if not present
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

// formatStatus converts a GenericXdsConfig status to a human-readable string
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

// handleAndGetXdsConfigs processes both new and legacy XDS config formats
func handleAndGetXdsConfigs(clientConfig *xdsstatus.ClientConfig) []*xdsstatus.ClientConfig_GenericXdsConfig {
	// Try new format first
	if clientConfig.GetGenericXdsConfigs() != nil {
		return clientConfig.GetGenericXdsConfigs()
	}

	// Fall back to legacy format (deprecated GetXdsConfig)
	configs := make([]*xdsstatus.ClientConfig_GenericXdsConfig, 0)
	for _, config := range clientConfig.GetXdsConfig() { //nolint:staticcheck // backward compatibility for legacy data
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
