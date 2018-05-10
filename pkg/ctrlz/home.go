// Copyright 2018 Istio Authors
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

package ctrlz

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"istio.io/istio/pkg/ctrlz/fw"
)

var mimeTypes = map[string]string{
	".css": "text/css; charset=utf-8",
	".svg": "image/svg+xml; charset=utf-8",
	".ico": "image/x-icon",
	".png": "image/png",
	".js":  "application/javascript",
}

type homeInfo struct {
	ProcessName string
	HeapSize    uint64
	NumGC       uint32
	CurrentTime int64
	Hostname    string
	IP          string
}

func getHomeInfo() *homeInfo {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	hostName, _ := os.Hostname()

	return &homeInfo{
		ProcessName: os.Args[0],
		HeapSize:    ms.HeapAlloc,
		NumGC:       ms.NumGC,
		CurrentTime: time.Now().UnixNano(),
		Hostname:    hostName,
		IP:          getLocalIP(),
	}
}

func registerHome(router *mux.Router, layout *template.Template) {
	homeTmpl := template.Must(template.Must(layout.Clone()).Parse(string(MustAsset("assets/templates/home.html"))))
	errorTmpl := template.Must(template.Must(layout.Clone()).Parse(string(MustAsset("assets/templates/404.html"))))

	_ = router.NewRoute().PathPrefix("/").Methods("GET").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" {
			// home page
			fw.RenderHTML(w, homeTmpl, getHomeInfo())
		} else if req.URL.Path == "/homej" || req.URL.Path == "/homej/" {
			fw.RenderJSON(w, http.StatusOK, getHomeInfo())
		} else if a, err := Asset("assets/static" + req.URL.Path); err == nil {
			// static asset
			ext := strings.ToLower(filepath.Ext(req.URL.Path))
			if mime, ok := mimeTypes[ext]; ok {
				w.Header().Set("Content-Type", mime)
			}
			w.Write(a)
		} else {
			// 'not found' page
			w.WriteHeader(http.StatusNotFound)
			fw.RenderHTML(w, errorTmpl, nil)
		}
	})

	_ = router.NewRoute().Methods("PUT").Path("/homej/exit").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		time.AfterFunc(1*time.Second, func() {
			os.Exit(0)
		})
	})
}
