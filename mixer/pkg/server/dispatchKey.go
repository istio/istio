// Copyright 2016 Google Inc.
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

package server

import (
	"fmt"

	"istio.io/mixer/pkg/attribute"
)

const (
	// ServiceName is a well-known name of a fact that is passed by the caller to specify
	// the particular config that should be used when handling a request.
	ServiceName = "serviceName"

	// PeerID is a well-known name of a fact that is passed by the caller to specify
	// the id of the client.
	PeerID = "peerId"
)

// DispatchKey is a structure that is used as an internal key within Mixer, based on well-known facts.
type DispatchKey struct {
	ServiceName string
	PeerID      string
}

// NewDispatchKey constructs and returns a new DispatchKey based on the incoming facts.
func NewDispatchKey(ac attribute.Bag) (DispatchKey, error) {
	serviceName, found := ac.String(ServiceName)
	if !found {
		return DispatchKey{}, fmt.Errorf("required attribute not found: %s", ServiceName)
	}
	peerID, found := ac.String(PeerID)
	if !found {
		return DispatchKey{}, fmt.Errorf("required attribute not found: %s", PeerID)
	}

	return DispatchKey{
		ServiceName: serviceName,
		PeerID:      peerID,
	}, nil
}
