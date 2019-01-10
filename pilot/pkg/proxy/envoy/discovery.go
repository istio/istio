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
	"github.com/prometheus/client_golang/prometheus"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

const (
	metricsNamespace   = "pilot"
	metricsSubsystem   = "discovery"
	metricLabelMethod  = "method"
	metricBuildVersion = "build_version"
)

var (
	// Save the build version information.
	buildVersion = version.Info.String()

	callCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "calls",
			Help:      "Counter of individual method calls in Pilot",
		}, []string{metricLabelMethod, metricBuildVersion})
	errorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "errors",
			Help:      "Counter of errors encountered during a given method call within Pilot",
		}, []string{metricLabelMethod, metricBuildVersion})
	resourceBuckets = []float64{0, 10, 20, 30, 40, 50, 75, 100, 150, 250, 500, 1000, 10000}
	resourceCounter = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "resources",
			Help:      "Histogram of returned resource counts per method by Pilot",
			Buckets:   resourceBuckets,
		}, []string{metricLabelMethod, metricBuildVersion})
)

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
		observeResources(methodName, uint32(len(services)))
	}
}

func incCalls(methodName string) {
	callCounter.With(prometheus.Labels{
		metricLabelMethod:  methodName,
		metricBuildVersion: buildVersion,
	}).Inc()
}

func incErrors(methodName string) {
	errorCounter.With(prometheus.Labels{
		metricLabelMethod:  methodName,
		metricBuildVersion: buildVersion,
	}).Inc()
}

func observeResources(methodName string, count uint32) {
	resourceCounter.With(prometheus.Labels{
		metricLabelMethod:  methodName,
		metricBuildVersion: buildVersion,
	}).Observe(float64(count))
}

func errorResponse(methodName string, r *restful.Response, status int, msg string) {
	incErrors(methodName)
	log.Warn(msg)
	if err := r.WriteErrorString(status, msg); err != nil {
		log.Warna(err)
	}
}
