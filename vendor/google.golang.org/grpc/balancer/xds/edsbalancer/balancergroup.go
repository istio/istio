// +build go1.12

/*
 * Copyright 2019 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package edsbalancer

import (
	"context"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

type pickerState struct {
	weight uint32
	picker balancer.Picker
	state  connectivity.State
}

// balancerGroup takes a list of balancers, and make then into one balancer.
//
// Note that this struct doesn't implement balancer.Balancer, because it's not
// intended to be used directly as a balancer. It's expected to be used as a
// sub-balancer manager by a high level balancer.
//
// Updates from ClientConn are forwarded to sub-balancers
//  - service config update
//     - Not implemented
//  - address update
//  - subConn state change
//     - find the corresponding balancer and forward
//
// Actions from sub-balances are forwarded to parent ClientConn
//  - new/remove SubConn
//  - picker update and health states change
//     - sub-pickers are grouped into a group-picker
//     - aggregated connectivity state is the overall state of all pickers.
//  - resolveNow
type balancerGroup struct {
	cc balancer.ClientConn

	mu           sync.Mutex
	idToBalancer map[string]balancer.Balancer
	scToID       map[balancer.SubConn]string

	pickerMu sync.Mutex
	// All balancer IDs exist as keys in this map. If an ID is not in map, it's
	// either removed or never added.
	idToPickerState map[string]*pickerState
}

func newBalancerGroup(cc balancer.ClientConn) *balancerGroup {
	return &balancerGroup{
		cc: cc,

		scToID:          make(map[balancer.SubConn]string),
		idToBalancer:    make(map[string]balancer.Balancer),
		idToPickerState: make(map[string]*pickerState),
	}
}

// add adds a balancer built by builder to the group, with given id and weight.
func (bg *balancerGroup) add(id string, weight uint32, builder balancer.Builder) {
	bg.mu.Lock()
	if _, ok := bg.idToBalancer[id]; ok {
		bg.mu.Unlock()
		grpclog.Warningf("balancer group: adding a balancer with existing ID: %s", id)
		return
	}
	bg.mu.Unlock()
	bgcc := &balancerGroupCC{
		id:    id,
		group: bg,
	}
	b := builder.Build(bgcc, balancer.BuildOptions{})
	bg.mu.Lock()
	bg.idToBalancer[id] = b
	bg.mu.Unlock()

	bg.pickerMu.Lock()
	bg.idToPickerState[id] = &pickerState{
		weight: weight,
		// Start everything in IDLE. It's doesn't affect the overall state
		// because we don't count IDLE when aggregating (as opposite to e.g.
		// READY, 1 READY results in overall READY).
		state: connectivity.Idle,
	}
	bg.pickerMu.Unlock()
}

// remove removes the balancer with id from the group, and closes the balancer.
//
// It also removes the picker generated from this balancer from the picker
// group. It always results in a picker update.
func (bg *balancerGroup) remove(id string) {
	bg.mu.Lock()
	// Close balancer.
	if b, ok := bg.idToBalancer[id]; ok {
		b.Close()
		delete(bg.idToBalancer, id)
	}
	// Remove SubConns.
	for sc, bid := range bg.scToID {
		if bid == id {
			bg.cc.RemoveSubConn(sc)
			delete(bg.scToID, sc)
		}
	}
	bg.mu.Unlock()

	bg.pickerMu.Lock()
	// Remove id and picker from picker map. This also results in future updates
	// for this ID to be ignored.
	delete(bg.idToPickerState, id)
	// Update state and picker to reflect the changes.
	bg.cc.UpdateBalancerState(buildPickerAndState(bg.idToPickerState))
	bg.pickerMu.Unlock()
}

// changeWeight changes the weight of the balancer.
//
// NOTE: It always results in a picker update now. This probably isn't
// necessary. But it seems better to do the update because it's a change in the
// picker (which is balancer's snapshot).
func (bg *balancerGroup) changeWeight(id string, newWeight uint32) {
	bg.pickerMu.Lock()
	defer bg.pickerMu.Unlock()
	pState, ok := bg.idToPickerState[id]
	if !ok {
		return
	}
	if pState.weight == newWeight {
		return
	}
	pState.weight = newWeight
	// Update state and picker to reflect the changes.
	bg.cc.UpdateBalancerState(buildPickerAndState(bg.idToPickerState))
}

// Following are actions from the parent grpc.ClientConn, forward to sub-balancers.

// SubConn state change: find the corresponding balancer and then forward.
func (bg *balancerGroup) handleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	grpclog.Infof("balancer group: handle subconn state change: %p, %v", sc, state)
	bg.mu.Lock()
	var b balancer.Balancer
	if id, ok := bg.scToID[sc]; ok {
		if state == connectivity.Shutdown {
			// Only delete sc from the map when state changed to Shutdown.
			delete(bg.scToID, sc)
		}
		b = bg.idToBalancer[id]
	}
	bg.mu.Unlock()
	if b == nil {
		grpclog.Infof("balancer group: balancer not found for sc state change")
		return
	}
	if ub, ok := b.(balancer.V2Balancer); ok {
		ub.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: state})
	} else {
		b.HandleSubConnStateChange(sc, state)
	}
}

// Address change: forward to balancer.
func (bg *balancerGroup) handleResolvedAddrs(id string, addrs []resolver.Address) {
	bg.mu.Lock()
	b, ok := bg.idToBalancer[id]
	bg.mu.Unlock()
	if !ok {
		grpclog.Infof("balancer group: balancer with id %q not found", id)
		return
	}
	if ub, ok := b.(balancer.V2Balancer); ok {
		ub.UpdateResolverState(resolver.State{Addresses: addrs})
	} else {
		b.HandleResolvedAddrs(addrs, nil)
	}
}

// TODO: handleServiceConfig()
//
// For BNS address for slicer, comes from endpoint.Metadata. It will be sent
// from parent to sub-balancers as service config.

// Following are actions from sub-balancers, forward to ClientConn.

// newSubConn: forward to ClientConn, and also create a map from sc to balancer,
// so state update will find the right balancer.
//
// One note about removing SubConn: only forward to ClientConn, but not delete
// from map. Delete sc from the map only when state changes to Shutdown. Since
// it's just forwarding the action, there's no need for a removeSubConn()
// wrapper function.
func (bg *balancerGroup) newSubConn(id string, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	sc, err := bg.cc.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	bg.mu.Lock()
	bg.scToID[sc] = id
	bg.mu.Unlock()
	return sc, nil
}

// updateBalancerState: create an aggregated picker and an aggregated
// connectivity state, then forward to ClientConn.
func (bg *balancerGroup) updateBalancerState(id string, state connectivity.State, picker balancer.Picker) {
	grpclog.Infof("balancer group: update balancer state: %v, %v, %p", id, state, picker)
	bg.pickerMu.Lock()
	defer bg.pickerMu.Unlock()
	pickerSt, ok := bg.idToPickerState[id]
	if !ok {
		// All state starts in IDLE. If ID is not in map, it's either removed,
		// or never existed.
		grpclog.Infof("balancer group: pickerState not found when update picker/state")
		return
	}
	pickerSt.picker = picker
	pickerSt.state = state
	bg.cc.UpdateBalancerState(buildPickerAndState(bg.idToPickerState))
}

func (bg *balancerGroup) close() {
	bg.mu.Lock()
	for _, b := range bg.idToBalancer {
		b.Close()
	}
	// Also remove all SubConns.
	for sc := range bg.scToID {
		bg.cc.RemoveSubConn(sc)
	}
	bg.mu.Unlock()
}

func buildPickerAndState(m map[string]*pickerState) (connectivity.State, balancer.Picker) {
	var readyN, connectingN int
	readyPickerWithWeights := make([]pickerState, 0, len(m))
	for _, ps := range m {
		switch ps.state {
		case connectivity.Ready:
			readyN++
			readyPickerWithWeights = append(readyPickerWithWeights, *ps)
		case connectivity.Connecting:
			connectingN++
		}
	}
	var aggregatedState connectivity.State
	switch {
	case readyN > 0:
		aggregatedState = connectivity.Ready
	case connectingN > 0:
		aggregatedState = connectivity.Connecting
	default:
		aggregatedState = connectivity.TransientFailure
	}
	if aggregatedState == connectivity.TransientFailure {
		return aggregatedState, base.NewErrPicker(balancer.ErrTransientFailure)
	}
	return aggregatedState, newPickerGroup(readyPickerWithWeights)
}

type pickerGroup struct {
	readyPickerWithWeights []pickerState
	length                 int

	mu    sync.Mutex
	idx   int    // The index of the picker that will be picked
	count uint32 // The number of times the current picker has been picked.
}

// newPickerGroup takes pickers with weights, and group them into one picker.
//
// Note it only takes ready pickers. The map shouldn't contain non-ready
// pickers.
//
// TODO: (bg) confirm this is the expected behavior: non-ready balancers should
// be ignored when picking. Only ready balancers are picked.
func newPickerGroup(readyPickerWithWeights []pickerState) *pickerGroup {
	return &pickerGroup{
		readyPickerWithWeights: readyPickerWithWeights,
		length:                 len(readyPickerWithWeights),
	}
}

func (pg *pickerGroup) Pick(ctx context.Context, opts balancer.PickOptions) (conn balancer.SubConn, done func(balancer.DoneInfo), err error) {
	if pg.length <= 0 {
		return nil, nil, balancer.ErrNoSubConnAvailable
	}
	// TODO: the WRR algorithm needs a design.
	// MAYBE: move WRR implmentation to util.go as a separate struct.
	pg.mu.Lock()
	pickerSt := pg.readyPickerWithWeights[pg.idx]
	p := pickerSt.picker
	pg.count++
	if pg.count >= pickerSt.weight {
		pg.idx = (pg.idx + 1) % pg.length
		pg.count = 0
	}
	pg.mu.Unlock()
	return p.Pick(ctx, opts)
}

// balancerGroupCC implements the balancer.ClientConn API and get passed to each
// sub-balancer. It contains the sub-balancer ID, so the parent balancer can
// keep track of SubConn/pickers and the sub-balancers they belong to.
//
// Some of the actions are forwarded to the parent ClientConn with no change.
// Some are forward to balancer group with the sub-balancer ID.
type balancerGroupCC struct {
	id    string
	group *balancerGroup
}

func (bgcc *balancerGroupCC) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return bgcc.group.newSubConn(bgcc.id, addrs, opts)
}
func (bgcc *balancerGroupCC) RemoveSubConn(sc balancer.SubConn) {
	bgcc.group.cc.RemoveSubConn(sc)
}
func (bgcc *balancerGroupCC) UpdateBalancerState(state connectivity.State, picker balancer.Picker) {
	bgcc.group.updateBalancerState(bgcc.id, state, picker)
}
func (bgcc *balancerGroupCC) ResolveNow(opt resolver.ResolveNowOption) {
	bgcc.group.cc.ResolveNow(opt)
}
func (bgcc *balancerGroupCC) Target() string {
	return bgcc.group.cc.Target()
}
