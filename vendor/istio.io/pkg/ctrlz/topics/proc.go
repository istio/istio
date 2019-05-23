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

package topics

import (
	"html/template"
	"net/http"
	"os"
	"runtime"

	"istio.io/pkg/ctrlz/fw"
	"istio.io/pkg/ctrlz/topics/assets"
)

type procTopic struct {
}

// ProcTopic returns a ControlZ topic that allows visualization of process state.
func ProcTopic() fw.Topic {
	return procTopic{}
}

func (procTopic) Title() string {
	return "Process Info"
}

func (procTopic) Prefix() string {
	return "proc"
}

type procInfo struct {
	Egid       int    `json:"egid"`
	Euid       int    `json:"euid"`
	Gid        int    `json:"gid"`
	Groups     []int  `json:"groups"`
	Pid        int    `json:"pid"`
	Ppid       int    `json:"ppid"`
	UID        int    `json:"uid"`
	Wd         string `json:"wd"`
	Hostname   string `json:"hostname"`
	TempDir    string `json:"tempdir"`
	Threads    int    `json:"threads"`
	Goroutines int    `json:"goroutines"`
}

func getProcInfo() *procInfo {
	pi := procInfo{
		Egid:       os.Getegid(),
		Euid:       os.Geteuid(),
		Gid:        os.Getgid(),
		Pid:        os.Getpid(),
		Ppid:       os.Getppid(),
		UID:        os.Getuid(),
		TempDir:    os.TempDir(),
		Goroutines: runtime.NumGoroutine(),
	}

	pi.Groups, _ = os.Getgroups()
	pi.Wd, _ = os.Hostname()
	pi.Hostname, _ = os.Hostname()
	pi.Threads, _ = runtime.ThreadCreateProfile(nil)

	return &pi
}

func (procTopic) Activate(context fw.TopicContext) {
	tmpl := template.Must(context.Layout().Parse(string(assets.MustAsset("templates/proc.html"))))

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderHTML(w, tmpl, getProcInfo())
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderJSON(w, http.StatusOK, getProcInfo())
	})
}
