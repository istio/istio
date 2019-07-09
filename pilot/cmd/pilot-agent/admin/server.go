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

package admin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"

	"istio.io/pkg/log"
)

const (
	// quitPath is to notify the pilot agent to quit.
	quitPath = "/quitquitquit"
)

// Server provides an endpoint for handling admin commands.
type Server struct {
	addr      string
	adminPort uint16
}

// NewServer creates a new administrative server.
func NewServer(addr string, port uint16) (*Server, error) {
	return &Server{
		addr:      addr,
		adminPort: port,
	}, nil
}

// Run opens a the status port and begins accepting probes.
func (s *Server) Run(ctx context.Context) {
	log.Infof("Opening administrative port %d\n", s.adminPort)

	mux := http.NewServeMux()

	// Add the handler for quit handling.
	mux.HandleFunc(quitPath, s.handleQuit)

	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.addr, s.adminPort))
	if err != nil {
		log.Errorf("Error listening on status port: %v", err.Error())
		return
	}

	defer l.Close()

	go func() {
		if err := http.Serve(l, mux); err != nil {
			log.Errora(err)
			notifyExit()
		}
	}()

	// Wait for the agent to be shut down.
	<-ctx.Done()
	log.Info("Admin server has successfully terminated")
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != quitPath {
		http.Error(w, "request url path not right", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
	log.Infof("handling %s, and notify pilot-agent to exit", quitPath)
	notifyExit()
}

// notifyExit sends SIGTERM to itself
func notifyExit() {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		log.Errora(err)
	}
	log.Errora(p.Signal(syscall.SIGTERM))
}
