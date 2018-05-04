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

package cmd

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/howeyc/fsnotify"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"

	"istio.io/istio/galley/cmd/shared"
	pb "istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/log"

	"net/http"
	_ "net/http/pprof"
)

var (
	watchPath string
	watchPort uint
)

func watcheeCmd(fatalf shared.FormatFn) *cobra.Command {
	c := &cobra.Command{
		Use:   "watchee",
		Short: "Starts Galley component config distribution server",
		Run: func(cmd *cobra.Command, args []string) {
			if err := runWatchee(); err != nil {
				fatalf("Error during startup: %v", err)
			}
		},
	}
	c.PersistentFlags().StringVar(&watchPath, "path", "",
		"File or directory path from which to serve configuration")
	c.PersistentFlags().UintVar(&watchPort, "port", 9898,
		"Server port")
	return c
}

type ConfigState struct {
	Version   string // datetime
	Fragments map[string]*types.Any
}

type Watch struct {
	Path         string
	Push         chan *ConfigState
	ResumeMarker []byte
}

type Server struct {
	addWatchC    chan *Watch
	removeWatchC chan *Watch
	watches      map[string]map[*Watch]*Watch // by path

	// in-memory cache of component state sorted by path, i.e. <type>/<group>
	pathToState map[string]*ConfigState
	versionNum  int
}

func (s *Server) addWatch(w *Watch) {
	s.addWatchC <- w
}

func (s *Server) removeWatch(w *Watch) {
	s.removeWatchC <- w
}

func (s *Server) Watch(request *pb.Request, stream pb.Watcher_WatchServer) error {
	watch := &Watch{
		Path:         request.Target,
		Push:         make(chan *ConfigState, 1),
		ResumeMarker: request.ResumeMarker,
	}

	s.addWatch(watch)
	defer s.removeWatch(watch)

	peerAddr := "0.0.0.0"
	if p, ok := peer.FromContext(stream.Context()); ok {
		peerAddr = p.Addr.String()
	}

	log.Infof("new watch from %v: target=%v resume_marker=%v",
		peerAddr, request.Target, string(request.ResumeMarker))

	// XXX `ResumeMarker` is ignored in this implementation and the server
	// always provides the full snapshot to new watchers.

	// Retain previous config state to compute partial update batch
	// XXX per-watch cache
	prev := &ConfigState{Fragments: make(map[string]*types.Any)}

	for {
		select {
		case current := <-watch.Push:
			fmt.Printf("new push to %v\n", watch.Path)
			var batch pb.ChangeBatch

			add := func(name string, state pb.Change_State, fragment *types.Any) {
				change := &pb.Change{
					Element:      name,
					State:        state,
					Data:         fragment,
					Continued:    true,
					ResumeMarker: []byte(current.Version),
				}
				if state == pb.Change_EXISTS {
					change.Data = fragment
				}
				batch.Changes = append(batch.Changes, change)
			}

			// Add new fragments
			for name, fragment := range current.Fragments {
				if _, ok := prev.Fragments[name]; !ok {
					add(name, pb.Change_EXISTS, fragment)
				}
			}

			// Remove old fragments
			for name, fragment := range prev.Fragments {
				if _, ok := current.Fragments[name]; !ok {
					add(name, pb.Change_DOES_NOT_EXIST, fragment)
				}
			}

			// Indicate end of atomic versioned batch
			if n := len(batch.Changes); n > 0 {
				batch.Changes[n-1].Continued = false
			} else {
				// bad config push - signal error to caller?
				log.Info("no-op config push")
				for key, fragment := range current.Fragments {
					log.Infof("\tkey=%v type=%v\n", key, fragment.TypeUrl)
				}
				continue
			}

			prev = current
			if err := stream.Send(&batch); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

var (
	watchDebounceDelay  = 100 * time.Millisecond
	supportedExtensions = map[string]bool{
		".yaml": true,
		".yml":  true,
	}
)

func (s *Server) distribute() error {
	version := fmt.Sprintf("version-%v", s.versionNum)
	s.versionNum++

	s.pathToState = make(map[string]*ConfigState)
	for path, _ := range s.watches {
		s.pathToState[path] = &ConfigState{
			Version:   version,
			Fragments: make(map[string]*types.Any),
		}
	}

	// XXX - galley should have predefined group mappings before clients connect
	s.pathToState["/pilot"] = &ConfigState{
		Version:   version,
		Fragments: make(map[string]*types.Any),
	}

	// build fragments from unmarshaled data
	fragments := make(map[string]*types.Any)

	// walk full path - build full state
	err := filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// skip directories
		if info.IsDir() {
			return nil
		}
		if !supportedExtensions[filepath.Ext(path)] || (info.Mode()&os.ModeType) != 0 {
			return nil
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			log.Warnf("Failed to read %s: %v", path, err)
			return err
		}
		objs, _, err := crd.ParseInputs(string(data))
		if err != nil {
			log.Warnf("Failed to parse %s: %v", path, err)
			return err
		}

		for _, obj := range objs {
			fragment, err := types.MarshalAny(obj.Spec)
			if err != nil {
				return err
			}
			namespace := obj.Namespace
			if namespace == "" {
				namespace = "default"
			}
			element := fmt.Sprintf("%s/%s", namespace, obj.Name)
			fragments[element] = fragment
			log.Infof("adding %v", element)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// build config state per path
	for watchedPath, state := range s.pathToState {
		log.Infof("watchedPath=%v fragments %v", watchedPath, len(fragments))

		// XXX not correct - doesn't recoginize file seperates
		// if strings.HasPrefix(path, watchedPath) {
		// merge file data into snapshot for watched path
		for element, fragment := range fragments {
			if prev, ok := state.Fragments[element]; ok {
				log.Warnf("duplicate fragment %v prev=%v new=%v",
					element, prev, fragment)
			}
			state.Fragments[element] = fragment
		}
	}

	// distribute to watchers
	for path, watchers := range s.watches {
		state, ok := s.pathToState[path]
		if !ok {
			continue
		}

		for _, watch := range watchers {
			log.Infof("Pushing v=%v to %v (path=%v)",
				state.Version, watch, watch.Path)
			watch.Push <- state
		}
	}
	return nil
}

func runWatchee() error {
	go func() {
		log.Errorf("%v", http.ListenAndServe("localhost:6060", nil))
	}()

	if watchPath == "" {
		return errors.New("no file or directory path specified (see --path)")
	}
	if _, err := os.Stat(watchPath); os.IsNotExist(err) {
		return fmt.Errorf("%v doesn't exist", watchPath)
	}

	// XXX assume no symlinks to keep things simple
	fileWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := fileWatcher.Watch(watchPath); err != nil {
		return fmt.Errorf("could not open file watch on %v: %v", watchPath, err)
	}
	var (
		timerC <-chan time.Time
	)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", watchPort))
	if err != nil {
		panic(err.Error())
	}

	server := &Server{
		watches:      make(map[string]map[*Watch]*Watch),
		addWatchC:    make(chan *Watch),
		removeWatchC: make(chan *Watch),
		pathToState:  make(map[string]*ConfigState),
	}

	grpcServer := grpc.NewServer()
	pb.RegisterWatcherServer(grpcServer, server)
	go grpcServer.Serve(lis)

	if err := server.distribute(); err != nil {
		return err
	}

	for {
		select {
		case ev := <-fileWatcher.Event:
			log.Infof("update: %v", ev)
			// debounce file events
			if timerC == nil {
				timerC = time.After(watchDebounceDelay)
			}
		case w := <-server.addWatchC:
			log.Infof("Add watch %v", w)
			byPath, ok := server.watches[w.Path]
			if !ok {
				byPath = make(map[*Watch]*Watch)
				server.watches[w.Path] = byPath
			}
			byPath[w] = w

			switch string(w.ResumeMarker) {
			case "":
			case "now":
			default:

			}

			if state, ok := server.pathToState[w.Path]; ok {
				w.Push <- state
			}
		case w := <-server.removeWatchC:
			log.Infof("Remove watch %v", w)
			byPath, ok := server.watches[w.Path]
			if ok {
				delete(byPath, w)
				if len(byPath) == 0 {
					delete(server.watches, w.Path)
				}
			}
		case <-timerC:
			timerC = nil
			if err := server.distribute(); err != nil {
				log.Error(err.Error())
			}
		case err := <-fileWatcher.Error:
			log.Errorf("Watcher error: %v", err)
		}
	}
}
