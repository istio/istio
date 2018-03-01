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
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
)

const MountSubdir = "mount"
const SocketFilename = "socket"

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
	w := NewWatcher(b.searchPath)
	events := w.watch()
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
	for _, wl := range b.workloads.getAll() {
		wl.listener.Close()
	}
}

func (b *binder) handleEvent(e workloadEvent) {
	switch e.op {
	case Added:
		b.addListener(e.uid)
	case Removed:
		b.removeListener(e.uid)
	default:
		panic("Unknown workloadEvent op")
	}
}

func (b *binder) addListener(uid string) {
	w := workload{uid: uid}
	credPath := filepath.Join(b.searchPath, CredentialsSubdir, uid+CredentialsExtension)
	err := readCredentials(credPath, &w.creds)
	if err != nil {
		log.Printf("failed to read credentials at %s %v", credPath, err)
		return
	}
	sockPath := filepath.Join(b.searchPath, MountSubdir, uid, SocketFilename)
	_, err = os.Stat(sockPath)
	if !os.IsNotExist(err) {
		// file exists, try to delete it.
		err := os.Remove(sockPath)
		if err != nil {
			log.Printf("File %s exists and unable to remove.", sockPath)
			return
		}
	}
	lis, err := net.Listen("unix", sockPath)
	if err != nil {
		// TODO: consider adding retries
		log.Printf("failed to listen at %s %v", sockPath, err)
		return
	}
	w.listener = lis
	b.workloads.store(uid, w)

	// Start listening on a separate goroutine.
	go func() {
		if err := b.server.Serve(lis); err != nil {
			log.Printf("stopped listening on %s %v", sockPath, err)
		}
	}()

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
	w.listener.Close()
	b.workloads.delete(uid)
}
