package model

import (
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	httpConn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
)

type DestinationType string

const (
	Single DestinationType = "Single"

	Multiple DestinationType = "Multiple"
)

type BackendService struct {
	Namespace string
	Name      string
	Port      uint32
	Weight    int32
}

type IngressRoute struct {
	Name            string
	Host            string
	PathType        string
	Path            string
	DestinationType DestinationType
	// Deprecated
	ServiceName string
	ServiceList []BackendService
	Error       string
}

type IngressRouteCollection struct {
	Valid   []IngressRoute
	Invalid []IngressRoute
}

type IngressDomain struct {
	Host string

	// tls for HTTPS
	// default HTTP
	Protocol string

	// cluster id/namespace/name
	SecretName string

	// creation time of ingress resource
	CreationTime time.Time
	Error        string
}

type IngressDomainCollection struct {
	Valid   []IngressDomain
	Invalid []IngressDomain
}

func (ps *PushContext) GetGatewayByName(name string) *config.Config {
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return nil
	}

	for _, cfg := range ps.gatewayIndex.all {
		if cfg.Namespace == parts[0] && cfg.Name == parts[1] {
			return &cfg
		}
	}

	return nil
}

func (ps *PushContext) GetHTTPFiltersFromEnvoyFilter(node *Proxy) []*httpConn.HttpFilter {
	var out []*httpConn.HttpFilter
	envoyFilterWrapper := ps.EnvoyFilters(node)
	if envoyFilterWrapper != nil && len(envoyFilterWrapper.Patches) > 0 {
		httpFilters := envoyFilterWrapper.Patches[networking.EnvoyFilter_HTTP_FILTER]
		if len(httpFilters) > 0 {
			for _, filter := range httpFilters {
				if filter.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER ||
					filter.Operation == networking.EnvoyFilter_Patch_ADD ||
					filter.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE ||
					filter.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
					out = append(out, proto.Clone(filter.Value).(*httpConn.HttpFilter))
				}
			}
		}
	}

	return out
}

func (ps *PushContext) GetExtensionConfigsFromEnvoyFilter(node *Proxy) []*core.TypedExtensionConfig {
	var out []*core.TypedExtensionConfig
	envoyFilterWrapper := ps.EnvoyFilters(node)
	if envoyFilterWrapper != nil && len(envoyFilterWrapper.Patches) > 0 {
		extensionConfigs := envoyFilterWrapper.Patches[networking.EnvoyFilter_EXTENSION_CONFIG]
		if len(extensionConfigs) > 0 {
			for _, extension := range extensionConfigs {
				if extension.Operation == networking.EnvoyFilter_Patch_ADD {
					out = append(out, proto.Clone(extension.Value).(*core.TypedExtensionConfig))
				}
			}
		}
	}

	return out
}
