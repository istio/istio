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

package watch

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"istio.io/istio/pkg/log"

	pb "istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
)

const ()

type Watcher struct {
	descriptor model.ConfigDescriptor
	host       string
	path       string

	handlers map[string]func(model.Config, model.Event)

	mu      sync.RWMutex
	version string
	store   map[string]map[string]map[string]model.Config
}

func NewController(rawurl string, descriptor model.ConfigDescriptor) (*Watcher, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		descriptor: descriptor,
		store:      make(map[string]map[string]map[string]model.Config),
		host:       u.Host,
		path:       u.Path,
		handlers:   make(map[string]func(model.Config, model.Event)),
	}
	return w, nil
}

// ConfigDescriptor exposes the configuration type schema known by the config store.
// The type schema defines the bidrectional mapping between configuration
// types and the protobuf encoding schema.
func (w *Watcher) ConfigDescriptor() model.ConfigDescriptor {
	return w.descriptor
}

// Get retrieves a configuration element by a type and a key
func (w *Watcher) Get(typ, name, namespace string) (config *model.Config, exists bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	byType, ok := w.store[typ]
	if !ok {
		return nil, false
	}
	byNamespace, ok := byType[namespace]
	if !ok {
		return nil, false
	}
	obj, ok := byNamespace[name]
	return &obj, ok
}

// List returns objects by type and namespace.
// Use "" for the namespace to list across namespaces.
func (w *Watcher) List(typ, namespace string) ([]model.Config, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	byType, ok := w.store[typ]
	if !ok {
		return nil, nil
	}

	var objs []model.Config

	if namespace == "" {
		for _, byNamespace := range byType {
			for _, obj := range byNamespace {
				objs = append(objs, obj)
			}
		}
	} else {
		byNamespace, ok := byType[namespace]
		if !ok {
			return nil, nil
		}
		for _, obj := range byNamespace {
			objs = append(objs, obj)
		}
	}

	return objs, nil
}

// Create adds a new configuration object to the store. If an object with the
// same name and namespace for the type already exists, the operation fails
// with no side effects.
func (w *Watcher) Create(config model.Config) (revision string, err error) {
	panic("Create() not supported")
}

// Update modifies an existing configuration object in the store.  Update
// requires that the object has been created.  Resource version prevents
// overriding a value that has been changed between prior _Get_ and _Put_
// operation to achieve optimistic concurrency. This method returns a new
// revision if the operation succeeds.
func (w *Watcher) Update(config model.Config) (newRevision string, err error) {
	panic("Update() not supported")
}

// Delete removes an object from the store by key
func (w *Watcher) Delete(typ, name, namespace string) error {
	panic("Delete() not supported")
}

// RegisterEventHandler adds a handler to receive config update events for a
// configuration type
func (w *Watcher) RegisterEventHandler(typ string, handler func(model.Config, model.Event)) {
	w.handlers[typ] = handler
	log.Infof("RegisterEventHandler: %v %v", typ, handler)
}

// Run until a signal is received
func (w *Watcher) Run(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-stop
		cancel()
	}()

	defer func() {
		if err := ctx.Err(); err != nil {
			log.Infof("context.Err() = %v", err)
		}
	}()

	options := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBackoffMaxDelay(time.Second),
		grpc.WithBlock(),
	}

	for {
		log.Warnf("Dialing %q", w.host)
		// start a new watch
		conn, err := grpc.Dial(w.host, options...)
		if err != nil {
			log.Warnf("Dial(%v) failed: %v", w.host, err)
			continue
		}

		log.Infof("Connected to %v: state=%v", w.host, conn.GetState())

		watch := pb.NewWatcherClient(conn)
		client, err := watch.Watch(ctx, &pb.Request{Target: w.path})
		if err != nil {
			log.Warnf("Watch(%v) failed: %v", w.path, err)
			continue
		}

		// staging around to accumate changes (add/update/remove)
		staging := make(map[string]*types.Any)

		for {
			batch, err := client.Recv() // start a new watch on error
			if err != nil {
				log.Infof("Watch closed: %v. reconnecting", err)
				break
			}

			fmt.Printf("ChangeBatch len = %v\n", len(batch.Changes))

			for _, change := range batch.Changes {
				switch change.State {
				case pb.Change_EXISTS: // add config element to staging store
					if prev, ok := staging[change.Element]; ok {
						log.Infof("UPDATE %v (prev %v)", change.Element, prev)
					} else {
						log.Infof("ADD %v", change.Element)
					}
					staging[change.Element] = change.Data
				case pb.Change_DOES_NOT_EXIST: // remove config element from staging store
					if prev, ok := staging[change.Element]; ok {
						log.Infof("DELETE %v (prev %v)", change.Element, prev)
						delete(staging, change.Element)
					} else {
						log.Infof("DELETE %v (does_not_exist)", change.Element)
					}
				}

				// build a versioned snapshot and apply
				if !change.Continued {
					store := make(map[string]map[string]map[string]model.Config)

					for element, data := range staging {
						typeUrlSuffix := strings.TrimPrefix(data.TypeUrl, "type.googleapis.com/")

						// validate type and schema
						schema, ok := w.descriptor.GetByMessageName(typeUrlSuffix)
						if !ok {
							log.Warnf("Snapshotting %q failed: unknown schema %v",
								element, typeUrlSuffix)
							continue
						}
						typ := proto.MessageType(typeUrlSuffix)
						if typ == nil {
							typ = gogoproto.MessageType(typeUrlSuffix)
							if typ == nil {
								log.Warnf("Snapshotting %q failed: unknown type %v",
									element, typeUrlSuffix)
								continue
							}
						}

						// unmarshal to type specific proto
						msg := reflect.New(typ.Elem()).Interface().(proto.Message)
						if err := types.UnmarshalAny(data, msg); err != nil {
							log.Warnf("Snapshotting %q failed: unmarshal failed for type %v",
								element, typeUrlSuffix)
							continue
						}

						// XXX hack - metadata should be included in the proto message
						// extract name and namespace from element name encoding
						s := strings.Split(element, "/")
						if len(s) != 2 {
							log.Warnf("Snapshotting %q failed: couldn't decode name and namespace from %v",
								element)
							continue
						}
						namespace, name := s[0], s[1]

						// insert config object into store
						byType, ok := store[schema.Type]
						if !ok {
							byType = make(map[string]map[string]model.Config)
							store[schema.Type] = byType
						}
						byNamespace, ok := byType[namespace]
						if !ok {
							byNamespace = make(map[string]model.Config)
							byType[namespace] = byNamespace
						}
						obj := model.Config{
							ConfigMeta: model.ConfigMeta{
								Type:            schema.Type,
								Group:           crd.ResourceGroup(&schema),
								Version:         schema.Version,
								Name:            name,
								Namespace:       namespace,
								Domain:          "cluster.local",
								Labels:          make(map[string]string),
								Annotations:     make(map[string]string),
								ResourceVersion: "TODO",
							},
							Spec: msg,
						}
						byNamespace[obj.Name] = obj
					}

					curVersion := string(change.ResumeMarker)

					// swap store store
					w.mu.Lock()
					w.store = store
					prevVersion := w.version
					w.version = curVersion
					w.mu.Unlock()

					log.Infof("New configuration version %q (prev %q)", curVersion, prevVersion)

					// dummy event to trigger cache flush for v1 discovery.
					for _, handler := range w.handlers {
						handler(model.Config{}, model.EventAdd)
						break
					}
				}
			}
		}
	}
}

// HasSynced returns true after initial cache synchronization is complete
func (w *Watcher) HasSynced() bool {
	return true
}
