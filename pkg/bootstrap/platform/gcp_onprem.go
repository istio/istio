package platform

import (
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"istio.io/pkg/log"
)

type gcpOnPremEnv struct {
	zone string
	jsonKey string
}

func NewGCPOnPremEnv(zone string, jsonKey string) Environment {
	return &gcpOnPremEnv{
		zone: zone,
		jsonKey: jsonKey,
	}
}

func (*gcpOnPremEnv) Metadata() map[string]string {
	return map[string]string{}
}

func (e *gcpOnPremEnv) Locality() *core.Locality {
	region, err := zoneToRegion(e.zone)
	if err != nil {
		log.Warnf("%v", err)
	}
	return &core.Locality{
		Region: region,
		Zone: e.zone,
	}
}

func (e *gcpOnPremEnv) Credentials() string {
	return e.jsonKey
}
