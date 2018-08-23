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

// Package promgen generates service graphs from a prometheus backend.
package promgen

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"istio.io/istio/addons/servicegraph"
)

const reqsFmt = "sum(rate(istio_requests_total{reporter=\"destination\"%s}[%s])) by (source_workload, destination_workload, source_app, destination_app)"
const tcpFmt = "sum(rate(istio_tcp_received_bytes_total{reporter=\"destination\"%s}[%s])) by (source_workload, destination_workload, source_app, destination_app)"
const emptyFilter = " > 0"

type genOpts struct {
	timeHorizon  string
	filterEmpty  bool
	dstNamespace string
	dstWorkload  string
	srcNamespace string
	srcWorkload  string
}

type promHandler struct {
	addr   string
	static *servicegraph.Static
	writer servicegraph.SerializeFn
}

// NewPromHandler returns a new http.Handler that will serve servicegraph data
// based on queries against a prometheus backend.
func NewPromHandler(addr string, static *servicegraph.Static, writer servicegraph.SerializeFn) http.Handler {
	return &promHandler{addr, static, writer}
}

func (p *promHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	timeHorizon := r.URL.Query().Get("time_horizon")
	if timeHorizon == "" {
		timeHorizon = "5m"
	}
	filterEmpty := false
	filterEmptyStr := r.URL.Query().Get("filter_empty")
	if filterEmptyStr == "true" {
		filterEmpty = true
	}

	dstNamespace := r.URL.Query().Get("destination_namespace")
	dstWorkload := r.URL.Query().Get("destination_workload")
	srcNamespace := r.URL.Query().Get("source_namespace")
	srcWorkload := r.URL.Query().Get("source_workload")
	// validate time_horizon
	if _, err := model.ParseDuration(timeHorizon); err != nil {
		writeError(w, fmt.Errorf("could not parse time_horizon: %v", err))
		return
	}
	g, err := p.generate(genOpts{
		timeHorizon,
		filterEmpty,
		dstNamespace,
		dstWorkload,
		srcNamespace,
		srcWorkload,
	})
	g.Merge(p.static)
	if err != nil {
		writeError(w, err)
		return
	}
	err = p.writer(w, g)
	if err != nil {
		writeError(w, err)
		return
	}
}

func writeError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	_, writeErr := w.Write([]byte(err.Error()))
	log.Print(writeErr)
}

func (p *promHandler) generate(opts genOpts) (*servicegraph.Dynamic, error) {

	client, err := api.NewClient(api.Config{Address: p.addr})
	if err != nil {
		return nil, err
	}
	api := v1.NewAPI(client)
	serviceFilter := generateServiceFilter(opts)
	query := fmt.Sprintf(reqsFmt, serviceFilter, opts.timeHorizon)
	if opts.filterEmpty {
		query += emptyFilter
	}
	graph, err := extractGraph(api, query, "reqs/sec")
	if err != nil {
		return nil, err
	}
	query = fmt.Sprintf(tcpFmt, serviceFilter, opts.timeHorizon)
	if opts.filterEmpty {
		query += emptyFilter
	}
	tcpGraph, err := extractGraph(api, query, "bytes/sec")
	if err != nil {
		return nil, err
	}
	return merge(graph, tcpGraph)
}

func generateServiceFilter(opts genOpts) string {
	filterParams := make([]string, 0, 4)
	if opts.dstNamespace != "" {
		filterParams = append(filterParams, "destination_namespace=\""+opts.dstNamespace+"\"")
	}
	if opts.dstWorkload != "" {
		filterParams = append(filterParams, "destination_workload=\""+opts.dstWorkload+"\"")
	}
	if opts.srcNamespace != "" {
		filterParams = append(filterParams, "source_namespace=\""+opts.srcNamespace+"\"")
	}
	if opts.srcWorkload != "" {
		filterParams = append(filterParams, "source_workload=\""+opts.srcWorkload+"\"")
	}
	filterStr := strings.Join(filterParams, ", ")
	if filterStr != "" {
		filterStr = ", " + filterStr
	}
	return filterStr
}

func merge(g1, g2 *servicegraph.Dynamic) (*servicegraph.Dynamic, error) {
	d := servicegraph.Dynamic{Nodes: map[string]struct{}{}, Edges: []*servicegraph.Edge{}}
	d.Edges = append(d.Edges, g1.Edges...)
	d.Edges = append(d.Edges, g2.Edges...)
	for nodeName, nodeValue := range g1.Nodes {
		d.Nodes[nodeName] = nodeValue
	}
	for nodeName, nodeValue := range g2.Nodes {
		d.Nodes[nodeName] = nodeValue
	}
	return &d, nil
}

func extractGraph(api v1.API, query, label string) (*servicegraph.Dynamic, error) {
	val, err := api.Query(context.Background(), query, time.Now())
	if err != nil {
		return nil, err
	}
	switch val.Type() {
	case model.ValVector:
		matrix := val.(model.Vector)
		d := servicegraph.Dynamic{Nodes: map[string]struct{}{}, Edges: []*servicegraph.Edge{}}
		for _, sample := range matrix {
			// todo: add error checking here
			metric := sample.Metric
			srcWorkload := string(metric["source_workload"])
			src := string(metric["source_app"])
			dstWorkload := string(metric["destination_workload"])
			dst := string(metric["destination_app"])

			value := sample.Value
			d.AddEdge(
				src+" ("+srcWorkload+")",
				dst+" ("+dstWorkload+")",
				servicegraph.Attributes{
					label: strconv.FormatFloat(float64(value), 'f', 6, 64),
				})
		}
		return &d, nil
	default:
		return nil, fmt.Errorf("unknown value type returned from query: %#v", val)
	}
}
