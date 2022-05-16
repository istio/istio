package configdump

import (
	"fmt"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
)

// GetEndpointsConfigDump retrieves the listener config dump from the ConfigDump
func (w *Wrapper) GetEndpointsConfigDump() (*adminapi.EndpointsConfigDump, error) {
	endpointsDumpAny, err := w.getSection(endpoints)
	if err != nil {
		return nil, fmt.Errorf("endpoints not found (was include_eds=true used?): %v", err)
	}
	endpointsDump := &adminapi.EndpointsConfigDump{}
	err = endpointsDumpAny.UnmarshalTo(endpointsDump)
	if err != nil {
		return nil, err
	}
	return endpointsDump, nil
}
