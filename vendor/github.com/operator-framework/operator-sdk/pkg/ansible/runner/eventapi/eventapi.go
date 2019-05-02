// Copyright 2018 The Operator-SDK Authors
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

package eventapi

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/operator-framework/operator-sdk/internal/util/fileutil"

	"github.com/go-logr/logr"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// EventReceiver serves the event API
type EventReceiver struct {
	// Events is the channel used by the event API handler to send JobEvents
	// back to the runner, or whatever code is using this receiver.
	Events chan JobEvent

	// SocketPath is the path on the filesystem to a unix streaming socket
	SocketPath string

	// URLPath is the path portion of the url at which events should be
	// received. For example, "/events/"
	URLPath string

	// server is the http.Server instance that serves the event API. It must be
	// closed.
	server io.Closer

	// stopped indicates if this receiver has permanently stopped receiving
	// events. When true, requests to POST an event will receive a "410 Gone"
	// response, and the body will be ignored.
	stopped bool

	// mutex controls access to the "stopped" bool above, ensuring that writes
	// are goroutine-safe.
	mutex sync.RWMutex

	// ident is the unique identifier for a particular run of ansible-runner
	ident string

	// logger holds a logger that has some fields already set
	logger logr.Logger
}

func New(ident string, errChan chan<- error) (*EventReceiver, error) {
	sockPath := fmt.Sprintf("/tmp/ansibleoperator-%s", ident)
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}

	rec := EventReceiver{
		Events:     make(chan JobEvent, 1000),
		SocketPath: sockPath,
		URLPath:    "/events/",
		ident:      ident,
		logger:     logf.Log.WithName("eventapi").WithValues("job", ident),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(rec.URLPath, rec.handleEvents)
	srv := http.Server{Handler: mux}
	rec.server = &srv

	go func() {
		errChan <- srv.Serve(listener)
	}()
	return &rec, nil
}

// Close ensures that appropriate resources are cleaned up, such as any unix
// streaming socket that may be in use. Close must be called.
func (e *EventReceiver) Close() {
	e.mutex.Lock()
	e.stopped = true
	e.mutex.Unlock()
	e.logger.V(1).Info("Event API stopped")
	if err := e.server.Close(); err != nil && !fileutil.IsClosedError(err) {
		e.logger.Error(err, "Failed to close event receiver")
	}
	close(e.Events)
}

func (e *EventReceiver) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != e.URLPath {
		http.NotFound(w, r)
		e.logger.Info("Path not found", "code", "404", "Request.Path", r.URL.Path)
		return
	}

	if r.Method != http.MethodPost {
		e.logger.Info("Method not allowed", "code", "405", "Request.Method", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ct := r.Header.Get("content-type")
	if strings.Split(ct, ";")[0] != "application/json" {
		e.logger.Info("Wrong content type", "code", "415", "Request.Content-Type", ct)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		if _, err := w.Write([]byte("The content-type must be \"application/json\"")); err != nil {
			e.logger.Error(err, "Failed to write response body")
		}
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		e.logger.Error(err, "Could not read request body", "code", "500")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	event := JobEvent{}
	err = json.Unmarshal(body, &event)
	if err != nil {
		e.logger.Info("Could not deserialize body.", "code", "400", "Error", err)
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte("Could not deserialize body as JSON")); err != nil {
			e.logger.Error(err, "Failed to write response body")
		}
		return
	}

	// Guarantee that the Events channel will not be written to if stopped ==
	// true, because in that case the channel has been closed.
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	if e.stopped {
		e.mutex.RUnlock()
		w.WriteHeader(http.StatusGone)
		e.logger.Info("Stopped and not accepting additional events for this job", "code", "410")
		return
	}
	// ansible-runner sends "status events" and "ansible events". The "status
	// events" signify a change in the state of ansible-runner itself, which
	// we're not currently interested in.
	// https://ansible-runner.readthedocs.io/en/latest/external_interface.html#event-structure
	if event.UUID == "" {
		e.logger.V(1).Info("Dropping event that is not a JobEvent")
	} else {
		// timeout if the channel blocks for too long
		timeout := time.NewTimer(10 * time.Second)
		select {
		case e.Events <- event:
		case <-timeout.C:
			e.logger.Info("Timed out writing event to channel", "code", "500")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = timeout.Stop()
	}
	w.WriteHeader(http.StatusNoContent)
}
