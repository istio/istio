package monitoring

import (
	"context"
	"net"
)

// This tag contains the mappings to the labels used in various
// backends for the data. Current known backends: prom, stackdriver.
const TagName = "istio"

type Workload struct {
	IPAddress         net.IP `istio:"prom=ip"`
	Name              string `istio:"prom=workload,stackdriver=workload_name"`
	Namespace         string `istio:"prom=workload_namespace,stackdriver=workload_namespace"`
	Owner             string `istio:"stackdriver=owner"`
	App               string `istio:"prom=app,stackdriver=canonical_service_name"` // should these (app, version) be removed for duplication?
	Version           string `istio:"prom=version,stackdriver=canonical_revision"`
	Principal         string `istio:"prom=principal,stackdriver=principal"`
	CanonicalService  string `istio:"prom=canonical_service,stackdriver=canonical_service_name"`
	CanonicalRevision string `istio:"prom=canonical_revision,stackdriver=canonical_revision"`
	InstanceName      string
	Location          string
	Container         string
	Port              string `istio:"stackdriver=port"`
}

type Protocol int

const (
	UnknownProtocol Protocol = iota
	HTTP
	GRPC
	TCP
)

type Request struct {
	Protocol                    Protocol `istio:"prom=protocol,stackdriver=request_protocol"`
	ResponseCode                string   `istio:"prom=response_code,stackdriver=response_code"`
	ResponseFlags               string   `istio:"prom=response_flags"`
	DestinationService          string   `istio:"prom=destination_service"`
	DestinationServiceName      string   `istio:"prom=destination_service_name,stackdriver=destination_service_name"`
	DestinationServiceNamespace string   `istio:"prom=destination_service_namespace,stackdriver=destination_service_namespace"`
}

type SecurityPolicy string

const (
	UnknownSecurityPolicy SecurityPolicy = "unknown"
	NoSecurityPolicy                     = "none"
	MutualTLS                            = "mutual_tls" // this is probably going to be an issue, need to toLower() or toUpper in appropriate place
)

type TrafficContext struct {
	MeshUID                  string         `istio:"stackdriver=mesh_uid`
	Reporter                 string         `istio:"prom=reporter"`
	SourceCluster            string         `istio:"prom=source_cluster"`
	DestinationCluster       string         `istio:"prom=destination_cluster"`
	ConnectionSecurityPolicy SecurityPolicy `istio:"prom=connection_security_policy,stackdriver=service_authentication_policy"`
}

type TrafficDimensions struct {
	Source      Workload
	Destination Workload
	Request     Request
	Context     TrafficContext
}

type MetricsService interface {
	// Requests returns the number of recorded requests matching the dimensions
	// passed in.
	Requests(context.Context, TrafficDimensions) (float64, error)

	// probably need a break-glass Query(ctx, string) (float64, error) method
	// if this was going to be expanded.

	// not sure quite what the control-plane metrics querying should look like
	// yet. Those tend to be less about dimensions, and more about the individual
	// stat. May be enough to have methods like: ProxyConvergenceTime(), etc.,
	// as needed.
}
