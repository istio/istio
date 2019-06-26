// Copyright 2017 Istio Authors
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

package envoy

import (
	"net/http"
	"net/http/pprof"
	"sort"

	restful "github.com/emicklei/go-restful"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/monitoring"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

var (
	// Save the build version information.
	buildVersion = version.Info.String()

	methodTag = monitoring.MustCreateTag("method")
	buildTag  = monitoring.MustCreateTag("build_version")

	callCounter = monitoring.NewSum(
		"pilot_discovery_calls",
		"Individual method calls in Pilot",
		methodTag, buildTag,
	)

	errorCounter = monitoring.NewSum(
		"pilot_discovery_errors",
		"Errors encountered during a given method call within Pilot",
		methodTag, buildTag,
	)

	resourceCounter = monitoring.NewDistribution(
		"pilot_discovery_resources",
		"Returned resource counts per method by Pilot",
		[]float64{0, 10, 20, 30, 40, 50, 75, 100, 150, 250, 500, 1000, 10000},
		methodTag, buildTag,
	)
)

func init() {
	monitoring.MustRegisterViews(callCounter, errorCounter, resourceCounter)
}

// DiscoveryService publishes services, clusters, and routes for all proxies
type DiscoveryService struct {
	*model.Environment
	RestContainer *restful.Container
}

type host struct {
	Address string `json:"ip_address"`
	Port    int    `json:"port"`
	Tags    *tags  `json:"tags,omitempty"`
}

type tags struct {
	AZ     string `json:"az,omitempty"`
	Canary bool   `json:"canary,omitempty"`

	// Weight is an integer in the range [1, 100] or empty
	Weight int `json:"load_balancing_weight,omitempty"`
}

type keyAndService struct {
	Key   string  `json:"service-key"`
	Hosts []*host `json:"hosts"`
}

// DiscoveryServiceOptions contains options for create a new discovery
// service instance.
type DiscoveryServiceOptions struct {
	// The listening address for HTTP. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	HTTPAddr string

	// The listening address for GRPC. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	GrpcAddr string

	// The listening address for secure GRPC. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	// "" means disabling secure GRPC, used in test.
	SecureGrpcAddr string

	// The listening address for the monitoring port. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	MonitoringAddr string

	EnableProfiling bool
	EnableCaching   bool
}

// NewDiscoveryService creates an Envoy discovery service on a given port
func NewDiscoveryService(environment *model.Environment, o DiscoveryServiceOptions) (*DiscoveryService, error) {
	out := &DiscoveryService{
		Environment: environment,
	}

	container := restful.NewContainer()
	if o.EnableProfiling {
		container.ServeMux.HandleFunc("/debug/pprof/", pprof.Index)
		container.ServeMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		container.ServeMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		container.ServeMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		container.ServeMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
	out.Register(container)
	out.RestContainer = container

	return out, nil
}

// Register adds routes a web service container. This is visible for testing purposes only.
func (ds *DiscoveryService) Register(container *restful.Container) {
	ws := &restful.WebService{}
	ws.Produces(restful.MIME_JSON)

	// List all known services (informational, not invoked by Envoy)
	ws.Route(ws.
		GET("/v1/registration").
		To(ds.ListAllEndpoints).
		Doc("Services in SDS"))

	container.Add(ws)
}

// ListAllEndpoints responds with all Services and is not restricted to a single service-key
// Deprecated - may be used by debug tools, mapped to /v1/registration
func (ds *DiscoveryService) ListAllEndpoints(_ *restful.Request, response *restful.Response) {
	methodName := "ListAllEndpoints"
	incCalls(methodName)

	services := make([]*keyAndService, 0)

	svcs, err := ds.Services()
	if err != nil {
		// If client experiences an error, 503 error will tell envoy to keep its current
		// cache and try again later
		errorResponse(methodName, response, http.StatusServiceUnavailable, "EDS "+err.Error())
		return
	}

	for _, service := range svcs {
		if !service.External() {
			for _, port := range service.Ports {
				hosts := make([]*host, 0)
				instances, err := ds.InstancesByPort(service.Hostname, port.Port, nil)
				if err != nil {
					// If client experiences an error, 503 error will tell envoy to keep its current
					// cache and try again later
					errorResponse(methodName, response, http.StatusInternalServerError, "EDS "+err.Error())
					return
				}
				for _, instance := range instances {
					hosts = append(hosts, &host{
						Address: instance.Endpoint.Address,
						Port:    instance.Endpoint.Port,
					})
				}
				services = append(services, &keyAndService{
					Key:   service.Key(port, nil),
					Hosts: hosts,
				})
			}
		}
	}

	// Sort servicesArray.  This is not strictly necessary, but discovery_test.go will
	// be comparing against a golden example using test/util/diff.go which does a textual comparison
	sort.Slice(services, func(i, j int) bool { return services[i].Key < services[j].Key })

	if err := response.WriteEntity(services); err != nil {
		incErrors(methodName)
		log.Warna(err)
	} else {
		observeResources(methodName, float64(len(services)))
	}
}

func incCalls(methodName string) {
	callCounter.With(buildTag.Value(buildVersion), methodTag.Value(methodName)).Increment()
}

func incErrors(methodName string) {
	errorCounter.With(buildTag.Value(buildVersion), methodTag.Value(methodName)).Increment()
}

func observeResources(methodName string, count float64) {
	resourceCounter.With(buildTag.Value(buildVersion), methodTag.Value(methodName)).Record(count)
}

func errorResponse(methodName string, r *restful.Response, status int, msg string) {
	incErrors(methodName)
	log.Warn(msg)
	if err := r.WriteErrorString(status, msg); err != nil {
		log.Warna(err)
	}
}
