// Copyright 2019 Istio Authors
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
//go:build windows
// +build windows

package topics

import (
	"net/http"

	"istio.io/istio/pkg/ctrlz/fw"
	"istio.io/istio/pkg/ctrlz/topics/assets"
)

type signalsTopic struct{}

// SignalsTopic returns a ControlZ topic that sends command signals to the process
func SignalsTopic() fw.Topic {
	return signalsTopic{}
}

func (signalsTopic) Title() string {
	return "Signals"
}

func (signalsTopic) Prefix() string {
	return "signal"
}

func (signalsTopic) Activate(context fw.TopicContext) {
	tmpl := assets.ParseTemplate(context.Layout(), "templates/signals.html")

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderHTML(w, tmpl, nil)
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("PUT", "POST").Path("/SIGUSR1").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		//@TODO send signal equivalent on windows:
		// err := syscall.Kill(os.Getpid(), syscall.SIGUSR1)
		w.WriteHeader(http.StatusAccepted)
	})
}
