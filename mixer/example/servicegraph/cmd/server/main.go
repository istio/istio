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

package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"

	"istio.io/istio/mixer/example/servicegraph"
	"istio.io/istio/mixer/example/servicegraph/dot"
	"istio.io/istio/mixer/example/servicegraph/promgen"
)

func writeJSON(w io.Writer, g *servicegraph.Dynamic) error {
	return json.NewEncoder(w).Encode(g)
}

type justFilesFilesystem struct {
	Fs http.FileSystem
}

func (fs justFilesFilesystem) Open(name string) (http.File, error) {
	f, err := fs.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	stat, _ := f.Stat()
	if stat.IsDir() {
		return nil, os.ErrNotExist
	}
	return f, nil
}

func (s *state) addNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotImplemented)
		_, err := w.Write([]byte("requests of this type not supported at this time"))
		if err != nil {
			log.Print(err)
		}
		return
	}
	nodeName := r.URL.Query().Get("name")
	if nodeName == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte("missing argument 'name'"))
		if err != nil {
			log.Print(err)
		}
		return
	}
	s.staticGraph.Nodes[nodeName] = struct{}{}
}

type state struct {
	staticGraph *servicegraph.Static
}

func main() {
	bindAddr := flag.String("bindAddr", ":8088", "Address to bind to for serving")
	promAddr := flag.String("prometheusAddr", "http://localhost:9090", "Address of prometheus instance for graph generation")
	assetDir := flag.String("assetDir", "example/servicegraph", "directory find assets to serve")
	flag.Parse()

	s := &state{staticGraph: &servicegraph.Static{Nodes: make(map[string]struct{})}}

	// don't allow directory listing
	jf := &justFilesFilesystem{http.Dir(*assetDir)}
	http.Handle("/", http.FileServer(jf))
	http.Handle("/graph", promgen.NewPromHandler(*promAddr, s.staticGraph, writeJSON))
	http.HandleFunc("/node", s.addNode)
	http.Handle("/dotgraph", promgen.NewPromHandler(*promAddr, s.staticGraph, dot.GenerateRaw))
	http.Handle("/dotviz", promgen.NewPromHandler(*promAddr, s.staticGraph, dot.GenerateHTML))

	log.Printf("Starting servicegraph service at %s", *bindAddr)
	log.Fatal(http.ListenAndServe(*bindAddr, nil))
}
