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

// Package ctrlz implements Istio's introspection facility. When components
// integrate with ControlZ, they automatically gain an IP port which allows operators
// to visualize and control a number of aspects of each process, including controlling
// logging scopes, viewing command-line options, memory use, etc. Additionally,
// the port implements a REST API allowing access and control over the same state.
//
// ControlZ is designed around the idea of "topics". A topic corresponds to the different
// parts of the UI. There are a set of built-in topics representing the core introspection
// functionality, and each component that uses ControlZ can add new topics specialized
// for their purpose.
package ctrlz

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"istio.io/pkg/ctrlz/assets"
	"istio.io/pkg/ctrlz/fw"
	"istio.io/pkg/ctrlz/topics"
	"istio.io/pkg/log"
)

var coreTopics = []fw.Topic{
	topics.ScopeTopic(),
	topics.MemTopic(),
	topics.EnvTopic(),
	topics.ProcTopic(),
	topics.ArgsTopic(),
	topics.VersionTopic(),
	topics.MetricsTopic(),
	topics.SignalsTopic(),
}

var allTopics []fw.Topic
var topicMutex sync.Mutex
var listeningTestProbe func()

// Server represents a running ControlZ instance.
type Server struct {
	listener   net.Listener
	shutdown   sync.WaitGroup
	httpServer http.Server
}

func augmentLayout(layout *template.Template, page string) *template.Template {
	return template.Must(layout.Parse(string(assets.MustAsset(page))))
}

func registerTopic(router *mux.Router, layout *template.Template, t fw.Topic) {
	htmlRouter := router.NewRoute().PathPrefix("/" + t.Prefix() + "z").Subrouter()
	jsonRouter := router.NewRoute().PathPrefix("/" + t.Prefix() + "j").Subrouter()

	tmpl := template.Must(template.Must(layout.Clone()).Parse("{{ define \"title\" }}" + t.Title() + "{{ end }}"))
	t.Activate(fw.NewContext(htmlRouter, jsonRouter, tmpl))
}

// getLocalIP returns a non loopback local IP of the host
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, address := range addrs {
		// check the address type and if it is not a loopback then return it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

type topic struct {
	Name string
	URL  string
}

func getTopics() []topic {
	topicMutex.Lock()
	defer topicMutex.Unlock()

	topics := make([]topic, 0, len(allTopics))
	for _, t := range allTopics {
		topics = append(topics, topic{Name: t.Title(), URL: "/" + t.Prefix() + "z/"})
	}

	return topics
}

// RegisterTopic registers a new Control-Z topic for the current process.
func RegisterTopic(t fw.Topic) {
	topicMutex.Lock()
	defer topicMutex.Unlock()

	allTopics = append(allTopics, t)
}

// Run starts up the ControlZ listeners.
//
// ControlZ uses the set of standard core topics, the
// supplied custom topics, as well as any topics registered
// via the RegisterTopic function.
func Run(o *Options, customTopics []fw.Topic) (*Server, error) {
	topicMutex.Lock()
	allTopics = append(allTopics, coreTopics...)
	allTopics = append(allTopics, customTopics...)
	topicMutex.Unlock()

	exec, _ := os.Executable()
	instance := exec + " - " + getLocalIP()

	funcs := template.FuncMap{
		"getTopics": getTopics,
	}

	baseLayout := template.Must(template.New("base").Parse(string(assets.MustAsset("templates/layouts/base.html"))))
	baseLayout = baseLayout.Funcs(funcs)
	baseLayout = template.Must(baseLayout.Parse("{{ define \"instance\" }}" + instance + "{{ end }}"))
	_ = augmentLayout(baseLayout, "templates/modules/header.html")
	_ = augmentLayout(baseLayout, "templates/modules/sidebar.html")
	_ = augmentLayout(baseLayout, "templates/modules/last-refresh.html")
	mainLayout := augmentLayout(template.Must(baseLayout.Clone()), "templates/layouts/main.html")

	router := mux.NewRouter()
	for _, t := range allTopics {
		registerTopic(router, mainLayout, t)
	}

	registerHome(router, mainLayout)

	addr := o.Address
	if addr == "*" {
		addr = ""
	}

	// Canonicalize the address and resolve a dynamic port if necessary
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr, o.Port))
	if err != nil {
		log.Errorf("Unable to start ControlZ: %v", err)
		return nil, err
	}

	s := &Server{
		listener: listener,
		httpServer: http.Server{
			Addr:           listener.Addr().(*net.TCPAddr).String(),
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxHeaderBytes: 1 << 20,
			Handler:        router,
		},
	}

	s.shutdown.Add(1)
	go s.listen()

	return s, nil
}

func (s *Server) listen() {
	log.Infof("ControlZ available at %s", s.httpServer.Addr)
	if listeningTestProbe != nil {
		go listeningTestProbe()
	}
	err := s.httpServer.Serve(s.listener)
	log.Infof("ControlZ terminated: %v", err)
	s.shutdown.Done()
}

// Close terminates ControlZ.
//
// Close is not normally used by programs that expose ControlZ, it is primarily intended to be
// used by tests.
func (s *Server) Close() {
	log.Info("Closing ControlZ")

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			log.Warnf("Error closing ControlZ: %v", err)
		}
		s.shutdown.Wait()
	}
}

func (s *Server) Address() string {
	return s.httpServer.Addr
}
