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

// Package dot provides serialization utilities for a servicegraph using the
// dot format.
package dot

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/template"

	"istio.io/mixer/example/servicegraph"
)

var htmlTmpl = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
    <title>Tiny example</title>
  </head>
  <body>

    <script src="/js/viz/viz-lite.js"></script>

    <script type="text/graphviz" id="cluster">
{{.}}
    </script>


    <script>

    document.body.innerHTML += Viz(document.getElementById("cluster").innerHTML, "svg");

    </script>

  </body>
</html>`

var charReplacer = strings.NewReplacer("/", "_", ".", "_", " ", "_")

// GenerateRaw writes out a dot graph in text form.
func GenerateRaw(w io.Writer, g *servicegraph.Dynamic) error {
	return generateDot(w, g)
}

// GenerateHTML writes out HTML that will visually display a graph based
// on a js rendering of dot.
func GenerateHTML(w io.Writer, g *servicegraph.Dynamic) error {
	var dotBuf bytes.Buffer
	if err := generateDot(&dotBuf, g); err != nil {
		return err
	}
	tmpl, err := template.New("html").Parse(htmlTmpl)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, dotBuf.String())
}

func generateDot(w io.Writer, g *servicegraph.Dynamic) error {
	var dotBuffer bytes.Buffer
	if _, err := dotBuffer.WriteString("digraph \"istio-servicegraph\" {\n"); err != nil {
		return err
	}
	for _, e := range g.Edges {
		label := labelStr(e.Labels)
		if _, err := dotBuffer.WriteString(fmt.Sprintf("%q -> %q [label=%q];\n", idStr(e.Source), idStr(e.Target), label)); err != nil {
			return err
		}
	}
	for n := range g.Nodes {
		if _, err := dotBuffer.WriteString(fmt.Sprintf("%q [label=%q];\n", idStr(n), n)); err != nil {
			return err
		}
	}
	if _, err := dotBuffer.WriteString("}\n"); err != nil {
		return err
	}
	_, err := w.Write(dotBuffer.Bytes())
	return err
}

func idStr(s string) string {
	return charReplacer.Replace(s)
}

func labelStr(m map[string]string) string {
	var labelBuf bytes.Buffer
	for k, v := range m {
		labelBuf.WriteString(fmt.Sprintf("%s: %s, ", k, v))
	}
	return strings.TrimRight(labelBuf.String(), ", ")
}
