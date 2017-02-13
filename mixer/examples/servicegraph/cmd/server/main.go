// Copyright 2017 Google Inc.
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

	"istio.io/mixer/examples/servicegraph"
	"istio.io/mixer/examples/servicegraph/dot"
	"istio.io/mixer/examples/servicegraph/promgen"
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

func main() {
	bindAddr := flag.String("bindAddr", ":8088", "Address to bind to for serving")
	promAddr := flag.String("prometheusAddr", "http://localhost:9090", "Address of prometheus instance for graph generation")
	flag.Parse()

	// don't allow directory listing
	jf := &justFilesFilesystem{http.Dir("examples/servicegraph")}
	http.Handle("/", http.FileServer(jf))
	http.Handle("/graph", promgen.NewPromHandler(*promAddr, writeJSON))
	http.Handle("/dotgraph", promgen.NewPromHandler(*promAddr, dot.GenerateRaw))
	http.Handle("/dotviz", promgen.NewPromHandler(*promAddr, dot.GenerateHTML))

	log.Printf("Starting servicegraph service at %s", *bindAddr)
	log.Fatal(http.ListenAndServe(*bindAddr, nil))
}
