// +build go1.12

/*
 *
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
 *
 */

// Package xds implements a balancer that communicates with a remote balancer using the Envoy xDS
// protocol.
package xds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/xds/edsbalancer"
	cdspb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/api/v2/cds"
	edspb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/api/v2/eds"

	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

const (
	defaultTimeout = 10 * time.Second
	xdsName        = "xds"
)

var (
	// This field is for testing purpose.
	// TODO: if later we make startupTimeout configurable through BuildOptions(maybe?), then we can remove
	// this field and configure through BuildOptions instead.
	startupTimeout = defaultTimeout
	newEDSBalancer = func(cc balancer.ClientConn) edsBalancerInterface {
		return edsbalancer.NewXDSBalancer(cc)
	}
)

func init() {
	balancer.Register(newXDSBalancerBuilder())
}

type xdsBalancerBuilder struct{}

func newXDSBalancerBuilder() balancer.Builder {
	return &xdsBalancerBuilder{}
}

func (b *xdsBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	ctx, cancel := context.WithCancel(context.Background())
	x := &xdsBalancer{
		ctx:             ctx,
		cancel:          cancel,
		buildOpts:       opts,
		startupTimeout:  startupTimeout,
		connStateMgr:    &connStateMgr{},
		startup:         true,
		grpcUpdate:      make(chan interface{}),
		xdsClientUpdate: make(chan interface{}),
		timer:           createDrainedTimer(), // initialized a timer that won't fire without reset
	}
	x.cc = &xdsClientConn{
		updateState: x.connStateMgr.updateState,
		ClientConn:  cc,
	}
	go x.run()
	return x
}

func (b *xdsBalancerBuilder) Name() string {
	return xdsName
}

// edsBalancerInterface defines the interface that edsBalancer must implement to
// communicate with xdsBalancer.
//
// It's implemented by the real eds balancer and a fake testing eds balancer.
type edsBalancerInterface interface {
	// HandleEDSResponse passes the received EDS message from traffic director to eds balancer.
	HandleEDSResponse(edsResp *edspb.ClusterLoadAssignment)
	// HandleChildPolicy updates the eds balancer the intra-cluster load balancing policy to use.
	HandleChildPolicy(name string, config json.RawMessage)
	// HandleSubConnStateChange handles state change for SubConn.
	HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State)
	// Close closes the eds balancer.
	Close()
}

// xdsBalancer manages xdsClient and the actual balancer that does load balancing (either edsBalancer,
// or fallback LB).
type xdsBalancer struct {
	cc                balancer.ClientConn // *xdsClientConn
	buildOpts         balancer.BuildOptions
	startupTimeout    time.Duration
	xdsStaleTimeout   *time.Duration
	connStateMgr      *connStateMgr
	ctx               context.Context
	cancel            context.CancelFunc
	startup           bool // startup indicates whether this xdsBalancer is in startup stage.
	inFallbackMonitor bool

	// xdsBalancer continuously monitor the channels below, and will handle events from them in sync.
	grpcUpdate      chan interface{}
	xdsClientUpdate chan interface{}
	timer           *time.Timer
	noSubConnAlert  <-chan struct{}

	client           *client    // may change when passed a different service config
	config           *xdsConfig // may change when passed a different service config
	xdsLB            edsBalancerInterface
	fallbackLB       balancer.Balancer
	fallbackInitData *addressUpdate // may change when HandleResolved address is called
}

func (x *xdsBalancer) startNewXDSClient(u *xdsConfig) {
	// If the xdsBalancer is in startup stage, then we need to apply the startup timeout for the first
	// xdsClient to get a response from the traffic director.
	if x.startup {
		x.startFallbackMonitoring()
	}

	// Whenever service config gives a new traffic director name, we need to create an xds client to
	// connect to it. However, previous xds client should not be closed until the new one successfully
	// connects to the traffic director (i.e. get an ADS response from the traffic director). Therefore,
	// we let each new client to be responsible to close its immediate predecessor. In this way,
	// xdsBalancer does not to implement complex synchronization to achieve the same purpose.
	prevClient := x.client
	// haveGotADS is true means, this xdsClient has got ADS response from director in the past, which
	// means it can close previous client if it hasn't and it now can send lose contact signal for
	// fallback monitoring.
	var haveGotADS bool

	// set up callbacks for the xds client.
	newADS := func(ctx context.Context, resp proto.Message) error {
		if !haveGotADS {
			if prevClient != nil {
				prevClient.close()
			}
			haveGotADS = true
		}
		return x.newADSResponse(ctx, resp)
	}
	loseContact := func(ctx context.Context) {
		// loseContact signal is only useful when the current xds client has received ADS response before,
		// and has not been closed by later xds client.
		if haveGotADS {
			select {
			case <-ctx.Done():
				return
			default:
			}
			x.loseContact(ctx)
		}
	}
	exitCleanup := func() {
		// Each xds client is responsible to close its predecessor if there's one. There are two paths
		// for a xds client to close its predecessor:
		// 1. Once it receives its first ADS response.
		// 2. It hasn't received its first ADS response yet, but its own successor has received ADS
		//    response (which triggers the exit of it). Therefore, it needs to close its predecessor if
		//    it has one.
		// Here the exitCleanup is for the 2nd path.
		if !haveGotADS && prevClient != nil {
			prevClient.close()
		}
	}
	x.client = newXDSClient(u.BalancerName, x.cc.Target(), u.ChildPolicy == nil, x.buildOpts, newADS, loseContact, exitCleanup)
	go x.client.run()
}

// run gets executed in a goroutine once xdsBalancer is created. It monitors updates from grpc,
// xdsClient and load balancer. It synchronizes the operations that happen inside xdsBalancer. It
// exits when xdsBalancer is closed.
func (x *xdsBalancer) run() {
	for {
		select {
		case update := <-x.grpcUpdate:
			x.handleGRPCUpdate(update)
		case update := <-x.xdsClientUpdate:
			x.handleXDSClientUpdate(update)
		case <-x.timer.C: // x.timer.C will block if we are not in fallback monitoring stage.
			x.switchFallback()
		case <-x.noSubConnAlert: // x.noSubConnAlert will block if we are not in fallback monitoring stage.
			x.switchFallback()
		case <-x.ctx.Done():
			if x.client != nil {
				x.client.close()
			}
			if x.xdsLB != nil {
				x.xdsLB.Close()
			}
			if x.fallbackLB != nil {
				x.fallbackLB.Close()
			}
			return
		}
	}
}

func (x *xdsBalancer) handleGRPCUpdate(update interface{}) {
	switch u := update.(type) {
	case *addressUpdate:
		if x.fallbackLB != nil {
			x.fallbackLB.HandleResolvedAddrs(u.addrs, u.err)
		}
		x.fallbackInitData = u
	case *subConnStateUpdate:
		if x.xdsLB != nil {
			x.xdsLB.HandleSubConnStateChange(u.sc, u.state)
		}
		if x.fallbackLB != nil {
			x.fallbackLB.HandleSubConnStateChange(u.sc, u.state)
		}
	case *xdsConfig:
		if x.config == nil {
			// The first time we get config, we just need to start the xdsClient.
			x.startNewXDSClient(u)
			x.config = u
			return
		}
		// With a different BalancerName, we need to create a new xdsClient.
		// If current or previous ChildPolicy is nil, then we also need to recreate a new xdsClient.
		// This is because with nil ChildPolicy xdsClient will do CDS request, while non-nil won't.
		if u.BalancerName != x.config.BalancerName || (u.ChildPolicy == nil) != (x.config.ChildPolicy == nil) {
			x.startNewXDSClient(u)
		}
		// We will update the xdsLB with the new child policy, if we got a different one and it's not nil.
		// The nil case will be handled when the CDS response gets processed, we will update xdsLB at that time.
		if !reflect.DeepEqual(u.ChildPolicy, x.config.ChildPolicy) && u.ChildPolicy != nil && x.xdsLB != nil {
			x.xdsLB.HandleChildPolicy(u.ChildPolicy.Name, u.ChildPolicy.Config)
		}
		if !reflect.DeepEqual(u.FallBackPolicy, x.config.FallBackPolicy) && x.fallbackLB != nil {
			x.fallbackLB.Close()
			x.startFallBackBalancer(u)
		}
		x.config = u
	default:
		// unreachable path
		panic("wrong update type")
	}
}

func (x *xdsBalancer) handleXDSClientUpdate(update interface{}) {
	switch u := update.(type) {
	case *cdsResp:
		select {
		case <-u.ctx.Done():
			return
		default:
		}
		x.cancelFallbackAndSwitchEDSBalancerIfNecessary()
		// TODO: Get the optional xds record stale timeout from OutlierDetection message. If not exist,
		// reset to 0.
		// x.xdsStaleTimeout = u.OutlierDetection.TO_BE_DEFINED_AND_ADDED
		x.xdsLB.HandleChildPolicy(u.resp.LbPolicy.String(), nil)
	case *edsResp:
		select {
		case <-u.ctx.Done():
			return
		default:
		}
		x.cancelFallbackAndSwitchEDSBalancerIfNecessary()
		x.xdsLB.HandleEDSResponse(u.resp)
	case *loseContact:
		select {
		case <-u.ctx.Done():
			return
		default:
		}
		// if we are already doing fallback monitoring, then we ignore new loseContact signal.
		if x.inFallbackMonitor {
			return
		}
		x.inFallbackMonitor = true
		x.startFallbackMonitoring()
	default:
		panic("unexpected xds client update type")
	}
}

type connStateMgr struct {
	mu       sync.Mutex
	curState connectivity.State
	notify   chan struct{}
}

func (c *connStateMgr) updateState(s connectivity.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.curState = s
	if s != connectivity.Ready && c.notify != nil {
		close(c.notify)
		c.notify = nil
	}
}

func (c *connStateMgr) notifyWhenNotReady() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.curState != connectivity.Ready {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	c.notify = make(chan struct{})
	return c.notify
}

// xdsClientConn wraps around the balancer.ClientConn passed in from grpc. The wrapping is to add
// functionality to get notification when no subconn is in READY state.
// TODO: once we have the change that keeps both edsbalancer and fallback balancer alive at the same
// time, we need to make sure to synchronize updates from both entities on the ClientConn.
type xdsClientConn struct {
	updateState func(s connectivity.State)
	balancer.ClientConn
}

func (w *xdsClientConn) UpdateBalancerState(s connectivity.State, p balancer.Picker) {
	w.updateState(s)
	w.ClientConn.UpdateBalancerState(s, p)
}

type addressUpdate struct {
	addrs []resolver.Address
	err   error
}

type subConnStateUpdate struct {
	sc    balancer.SubConn
	state connectivity.State
}

func (x *xdsBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	update := &subConnStateUpdate{
		sc:    sc,
		state: state,
	}
	select {
	case x.grpcUpdate <- update:
	case <-x.ctx.Done():
	}
}

func (x *xdsBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	update := &addressUpdate{
		addrs: addrs,
		err:   err,
	}
	select {
	case x.grpcUpdate <- update:
	case <-x.ctx.Done():
	}
}

// TODO: once the API is merged, check whether we need to change the function name/signature here.
func (x *xdsBalancer) HandleBalancerConfig(config json.RawMessage) error {
	var cfg xdsConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return errors.New("unable to unmarshal balancer config into xds config")
	}

	select {
	case x.grpcUpdate <- &cfg:
	case <-x.ctx.Done():
	}
	return nil
}

type cdsResp struct {
	ctx  context.Context
	resp *cdspb.Cluster
}

type edsResp struct {
	ctx  context.Context
	resp *edspb.ClusterLoadAssignment
}

func (x *xdsBalancer) newADSResponse(ctx context.Context, resp proto.Message) error {
	var update interface{}
	switch u := resp.(type) {
	case *cdspb.Cluster:
		if u.GetName() != x.cc.Target() {
			return fmt.Errorf("unmatched service name, got %s, want %s", u.GetName(), x.cc.Target())
		}
		if u.GetType() != cdspb.Cluster_EDS {
			return fmt.Errorf("unexpected service discovery type, got %v, want %v", u.GetType(), cdspb.Cluster_EDS)
		}
		update = &cdsResp{ctx: ctx, resp: u}
	case *edspb.ClusterLoadAssignment:
		// nothing to check
		update = &edsResp{ctx: ctx, resp: u}
	default:
		grpclog.Warningf("xdsBalancer: got a response that's neither CDS nor EDS, type = %T", u)
	}

	select {
	case x.xdsClientUpdate <- update:
	case <-x.ctx.Done():
	case <-ctx.Done():
	}

	return nil
}

type loseContact struct {
	ctx context.Context
}

func (x *xdsBalancer) loseContact(ctx context.Context) {
	select {
	case x.xdsClientUpdate <- &loseContact{ctx: ctx}:
	case <-x.ctx.Done():
	case <-ctx.Done():
	}
}

func (x *xdsBalancer) switchFallback() {
	if x.xdsLB != nil {
		x.xdsLB.Close()
		x.xdsLB = nil
	}
	x.startFallBackBalancer(x.config)
	x.cancelFallbackMonitoring()
}

// x.cancelFallbackAndSwitchEDSBalancerIfNecessary() will be no-op if we have a working xds client.
// It will cancel fallback monitoring if we are in fallback monitoring stage.
// If there's no running edsBalancer currently, it will create one and initialize it. Also, it will
// shutdown the fallback balancer if there's one running.
func (x *xdsBalancer) cancelFallbackAndSwitchEDSBalancerIfNecessary() {
	// xDS update will cancel fallback monitoring if we are in fallback monitoring stage.
	x.cancelFallbackMonitoring()

	// xDS update will switch balancer back to edsBalancer if we are in fallback.
	if x.xdsLB == nil {
		if x.fallbackLB != nil {
			x.fallbackLB.Close()
			x.fallbackLB = nil
		}
		x.xdsLB = newEDSBalancer(x.cc)
		if x.config.ChildPolicy != nil {
			x.xdsLB.HandleChildPolicy(x.config.ChildPolicy.Name, x.config.ChildPolicy.Config)
		}
	}
}

func (x *xdsBalancer) startFallBackBalancer(c *xdsConfig) {
	if c.FallBackPolicy == nil {
		x.startFallBackBalancer(&xdsConfig{
			FallBackPolicy: &loadBalancingConfig{
				Name: "round_robin",
			},
		})
		return
	}
	// builder will always be non-nil, since when parse JSON into xdsConfig, we check whether the specified
	// balancer is registered or not.
	builder := balancer.Get(c.FallBackPolicy.Name)
	x.fallbackLB = builder.Build(x.cc, x.buildOpts)
	if x.fallbackInitData != nil {
		// TODO: uncomment when HandleBalancerConfig API is merged.
		//x.fallbackLB.HandleBalancerConfig(c.FallBackPolicy.Config)
		x.fallbackLB.HandleResolvedAddrs(x.fallbackInitData.addrs, x.fallbackInitData.err)
	}
}

// There are three ways that could lead to fallback:
// 1. During startup (i.e. the first xds client is just created and attempts to contact the traffic
//    director), fallback if it has not received any response from the director within the configured
//    timeout.
// 2. After xds client loses contact with the remote, fallback if all connections to the backends are
//    lost (i.e. not in state READY).
// 3. After xds client loses contact with the remote, fallback if the stale eds timeout has been
//    configured through CDS and is timed out.
func (x *xdsBalancer) startFallbackMonitoring() {
	if x.startup {
		x.startup = false
		x.timer.Reset(x.startupTimeout)
		return
	}

	x.noSubConnAlert = x.connStateMgr.notifyWhenNotReady()
	if x.xdsStaleTimeout != nil {
		if !x.timer.Stop() {
			<-x.timer.C
		}
		x.timer.Reset(*x.xdsStaleTimeout)
	}
}

// There are two cases where fallback monitoring should be canceled:
// 1. xDS client returns a new ADS message.
// 2. fallback has been triggered.
func (x *xdsBalancer) cancelFallbackMonitoring() {
	if !x.timer.Stop() {
		select {
		case <-x.timer.C:
			// For cases where some fallback condition happens along with the timeout, but timeout loses
			// the race, so we need to drain the x.timer.C. thus we don't trigger fallback again.
		default:
			// if the timer timeout leads us here, then there's no thing to drain from x.timer.C.
		}
	}
	x.noSubConnAlert = nil
	x.inFallbackMonitor = false
}

func (x *xdsBalancer) Close() {
	x.cancel()
}

func createDrainedTimer() *time.Timer {
	timer := time.NewTimer(0 * time.Millisecond)
	// make sure initially the timer channel is blocking until reset.
	if !timer.Stop() {
		<-timer.C
	}
	return timer
}

type xdsConfig struct {
	BalancerName   string
	ChildPolicy    *loadBalancingConfig
	FallBackPolicy *loadBalancingConfig
}

// When unmarshalling json to xdsConfig, we iterate through the childPolicy/fallbackPolicy lists
// and select the first LB policy which has been registered to be stored in the returned xdsConfig.
func (p *xdsConfig) UnmarshalJSON(data []byte) error {
	var val map[string]json.RawMessage
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	for k, v := range val {
		switch k {
		case "balancerName":
			if err := json.Unmarshal(v, &p.BalancerName); err != nil {
				return err
			}
		case "childPolicy":
			var lbcfgs []*loadBalancingConfig
			if err := json.Unmarshal(v, &lbcfgs); err != nil {
				return err
			}
			for _, lbcfg := range lbcfgs {
				if balancer.Get(lbcfg.Name) != nil {
					p.ChildPolicy = lbcfg
					break
				}
			}
		case "fallbackPolicy":
			var lbcfgs []*loadBalancingConfig
			if err := json.Unmarshal(v, &lbcfgs); err != nil {
				return err
			}
			for _, lbcfg := range lbcfgs {
				if balancer.Get(lbcfg.Name) != nil {
					p.FallBackPolicy = lbcfg
					break
				}
			}
		}
	}
	return nil
}

func (p *xdsConfig) MarshalJSON() ([]byte, error) {
	return nil, nil
}

type loadBalancingConfig struct {
	Name   string
	Config json.RawMessage
}

func (l *loadBalancingConfig) MarshalJSON() ([]byte, error) {
	return nil, nil
}

func (l *loadBalancingConfig) UnmarshalJSON(data []byte) error {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	for name, config := range cfg {
		l.Name = name
		l.Config = config
	}
	return nil
}
