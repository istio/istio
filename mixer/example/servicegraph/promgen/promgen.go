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

	"github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/common/model"

	"istio.io/mixer/example/servicegraph"
)

const reqsFmt = "sum(rate(request_count[%s])) by (source_service, destination_service, source_version, destination_version)"
const tcpFmt = "sum(rate(tcp_bytes_received[%s])) by (source_service, destination_service, source_version, destination_version)"
const emptyFilter = " > 0"

type genOpts struct {
	timeHorizon string
	filterEmpty bool
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
	// validate time_horizon
	if _, err := model.ParseDuration(timeHorizon); err != nil {
		writeError(w, fmt.Errorf("could not parse time_horizon: %v", err))
		return
	}
	g, err := p.generate(genOpts{timeHorizon, filterEmpty})
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
	client, err := prometheus.New(prometheus.Config{Address: p.addr})
	if err != nil {
		return nil, err
	}
	api := prometheus.NewQueryAPI(client)
	query := fmt.Sprintf(reqsFmt, opts.timeHorizon)
	if opts.filterEmpty {
		query += emptyFilter
		fmt.Println(query)
	}
	graph, err := extractGraph(api, query, "reqs/sec")
	if err != nil {
		return nil, err
	}
	query = fmt.Sprintf(tcpFmt, opts.timeHorizon)
	if opts.filterEmpty {
		query += emptyFilter
		fmt.Println(query)
	}
	tcpGraph, err := extractGraph(api, query, "bytes/sec")
	if err != nil {
		return nil, err
	}
	return merge(graph, tcpGraph)
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

func extractGraph(api prometheus.QueryAPI, query, label string) (*servicegraph.Dynamic, error) {
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
			src := strings.Replace(string(metric["source_service"]), ".svc.cluster.local", "", -1)
			srcVer := string(metric["source_version"])
			dst := strings.Replace(string(metric["destination_service"]), ".svc.cluster.local", "", -1)
			dstVer := string(metric["destination_version"])

			value := sample.Value
			d.AddEdge(
				src+" ("+srcVer+")",
				dst+" ("+dstVer+")",
				servicegraph.Attributes{
					label: strconv.FormatFloat(float64(value), 'f', 6, 64),
				})
		}
		return &d, nil
	default:
		return nil, fmt.Errorf("unknown value type returned from query: %#v", val)
	}
}
