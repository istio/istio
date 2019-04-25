/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package repo

import (
	"fmt"
	htemplate "html/template"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/provenance"
)

const indexHTMLTemplate = `
<html>
<head>
	<title>Helm Repository</title>
</head>
<h1>Helm Charts Repository</h1>
<ul>
{{range $name, $ver := .Index.Entries}}
  <li>{{$name}}<ul>{{range $ver}}
    <li><a href="{{index .URLs 0}}">{{.Name}}-{{.Version}}</a></li>
  {{end}}</ul>
  </li>
{{end}}
</ul>
<body>
<p>Last Generated: {{.Index.Generated}}</p>
</body>
</html>
`

// RepositoryServer is an HTTP handler for serving a chart repository.
type RepositoryServer struct {
	RepoPath string
}

// ServeHTTP implements the http.Handler interface.
func (s *RepositoryServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Path
	switch uri {
	case "/", "/charts/", "/charts/index.html", "/charts/index":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		s.htmlIndex(w, r)
	default:
		file := strings.TrimPrefix(uri, "/charts/")
		http.ServeFile(w, r, filepath.Join(s.RepoPath, file))
	}
}

// StartLocalRepo starts a web server and serves files from the given path
func StartLocalRepo(path, address string) error {
	if address == "" {
		address = "127.0.0.1:8879"
	}
	s := &RepositoryServer{RepoPath: path}
	return http.ListenAndServe(address, s)
}

func (s *RepositoryServer) htmlIndex(w http.ResponseWriter, r *http.Request) {
	t := htemplate.Must(htemplate.New("index.html").Parse(indexHTMLTemplate))
	// load index
	lrp := filepath.Join(s.RepoPath, "index.yaml")
	i, err := LoadIndexFile(lrp)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	data := map[string]interface{}{
		"Index": i,
	}
	if err := t.Execute(w, data); err != nil {
		fmt.Fprintf(w, "Template error: %s", err)
	}
}

// AddChartToLocalRepo saves a chart in the given path and then reindexes the index file
func AddChartToLocalRepo(ch *chart.Chart, path string) error {
	_, err := chartutil.Save(ch, path)
	if err != nil {
		return err
	}
	return Reindex(ch, path+"/index.yaml")
}

// Reindex adds an entry to the index file at the given path
func Reindex(ch *chart.Chart, path string) error {
	name := ch.Metadata.Name + "-" + ch.Metadata.Version
	y, err := LoadIndexFile(path)
	if err != nil {
		return err
	}
	found := false
	for k := range y.Entries {
		if k == name {
			found = true
			break
		}
	}
	if !found {
		dig, err := provenance.DigestFile(path)
		if err != nil {
			return err
		}

		y.Add(ch.Metadata, name+".tgz", "http://127.0.0.1:8879/charts", "sha256:"+dig)

		out, err := yaml.Marshal(y)
		if err != nil {
			return err
		}

		ioutil.WriteFile(path, out, 0644)
	}
	return nil
}
