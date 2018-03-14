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

package binder

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"

	fv "istio.io/istio/security/pkg/flexvolume"
)

// MountSubdir is where the nodeagent set's up the socket for the workload pod
const MountSubdir = "mount"

// SocketFilename is the name of the socket file in the MountSubdir
const SocketFilename = "socket"

// Binder interface is used to have a gRPC server (node agent) to be notified of
// workload(s) credentials and create or delete per workload socket when notified
// of workloads.
type Binder interface {

	// Returns the gRPC server for this Binder. Used to register the service to be provided to workloads.
	Server() *grpc.Server

	// The path this Binder is searching for workload mounts on.
	SearchPath() string

	// Search for pod mounts to bind sockets in.
	// Send any value over the stop channel to gracefully cancel.
	SearchAndBind(stop <-chan interface{})
}

type binder struct {
	server     *grpc.Server
	searchPath string
	workloads  *workloadStore
}

type workload struct {
	uid      string
	listener net.Listener
	creds    Credentials
}

// NewBinder creates a new binder for use by node agent.
func NewBinder(searchPath string) Binder {
	ws := newWorkloadStore()
	return &binder{
		searchPath: searchPath,
		server:     grpc.NewServer(grpc.Creds(ws)),
		workloads:  ws}
}

func (b *binder) Server() *grpc.Server {
	return b.server
}

func (b *binder) SearchPath() string {
	return b.searchPath
}

func (b *binder) SearchAndBind(stop <-chan interface{}) {
	w := newWatcher(b.searchPath)
	stopWatch := make(chan bool)
	events := w.watch(stopWatch)
	var event workloadEvent
EventLoop:
	for {
		select {
		case event = <-events:
			b.handleEvent(event)
		case <-stop:
			break EventLoop
		}
	}
	// Got stop signal! Close any open sockets
	stopWatch <- true
	for _, wl := range b.workloads.getAll() {
		wl.listener.Close() //nolint: errcheck
	}
}

func (b *binder) handleEvent(e workloadEvent) {
	switch e.op {
	case added:
		if e := b.addListener(e.uid); e != nil {
			log.Printf(e.Error())
		}
	case removed:
		b.removeListener(e.uid)
	default:
		panic("Unknown workloadEvent op")
	}
}

func (b *binder) addListener(uid string) error {
	w := workload{uid: uid}
	credPath := filepath.Join(b.searchPath, credentialsSubdir, uid+fv.CredentialFileExtension)
	err := readCredentials(credPath, &w.creds)
	if err != nil {
		return fmt.Errorf("failed to read credentials at %s %v", credPath, err)
	}
	sockPath := filepath.Join(b.searchPath, MountSubdir, uid, SocketFilename)
	_, err = os.Stat(sockPath)
	if !os.IsNotExist(err) {
		// file exists, try to delete it.
		err := os.Remove(sockPath)
		if err != nil {
			return fmt.Errorf("file %s exists and unable to remove", sockPath)
		}
	}
	lis, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("failed to listen at %s error %s", sockPath, err.Error())
	}
	w.listener = lis
	b.workloads.store(uid, w)

	// Start listening on a separate goroutine.
	go func() {
		if err := b.server.Serve(lis); err != nil {
			log.Printf("stopped listening on %s %v", sockPath, err)
		}
	}()
	return nil
}

func readCredentials(path string, c *Credentials) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, &c.WorkloadCredentials)
	if err != nil {
		return err
	}
	return nil
}

func (b *binder) removeListener(uid string) {
	w := b.workloads.get(uid)
	// Closing the listener automatically removes it from the gRPC server.
	w.listener.Close() ////nolint: errcheck
	b.workloads.delete(uid)
}
