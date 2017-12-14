package runtime2

import (
	"context"
	"time"

	"github.com/gogo/protobuf/proto"
	"istio.io/istio/mixer/pkg/adapter"
	configpb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/log"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/dispatcher"
	"istio.io/istio/mixer/pkg/template"
)

// This file contains code to create new objects that are
// of package wide interest.

// New creates a new runtime Dispatcher
// Create a new controller and a dispatcher.
// Returns a ready to use dispatcher.
func New(
	gp *pool.GoroutinePool,
	handlerPool *pool.GoroutinePool,
	identityAttribute string,
	defaultConfigNamespace string, s store.Store2,
	adapterInfo map[string]*adapter.Info,
	templateInfo map[string]template.Info) (runtime.Dispatcher, error) {

	// controller will set Resolver before the dispatcher is used.
	d := dispatcher.New(gp)

	data, watchChan, err := startWatch(s, adapterInfo, templateInfo)
	if err != nil {
		return nil, err
	}

	c := NewController(templateInfo, adapterInfo, data, d, handlerPool)
	c.applyNewConfig()

	log.Infof("Config controller has started with %d config elements", len(data))

	go watchChanges(watchChan, c.onConfigChange)

	return d, nil
}

// startWatch registers with store, initiates a watch, and returns the current config state.
func startWatch(s store.Store2, adapterInfo map[string]*adapter.Info,
	templateInfo map[string]template.Info) (map[store.Key]*store.Resource, <-chan store.Event, error) {
	ctx := context.Background()
	kindMap := KindMap(adapterInfo, templateInfo)
	if err := s.Init(ctx, kindMap); err != nil {
		return nil, nil, err
	}
	// create channel before listing.
	watchChan, err := s.Watch(ctx)
	if err != nil {
		return nil, nil, err
	}
	return s.List(), watchChan, nil
}

// KindMap generates a map from object kind to its proto message.
func KindMap(adapterInfo map[string]*adapter.Info,
	templateInfo map[string]template.Info) map[string]proto.Message {
	kindMap := make(map[string]proto.Message)
	// typed instances
	for kind, info := range templateInfo {
		kindMap[kind] = info.CtrCfg
		log.Infof("template Kind: %s, %v", kind, info.CtrCfg)
	}
	// typed handlers
	for kind, info := range adapterInfo {
		kindMap[kind] = info.DefaultConfig
		log.Infof("adapter Kind: %s, %v", kind, info.DefaultConfig)
	}
	kindMap[config.RulesKind] = &configpb.Rule{}
	log.Infof("template Kind: %s", config.RulesKind)
	kindMap[config.AttributeManifestKind] = &configpb.AttributeManifest{}
	log.Infof("template Kind: %s", config.AttributeManifestKind)

	return kindMap
}

var watchFlushDuration = time.Second

// maxEvents is the likely maximum number of events
// we can expect in a second. It is used to avoid slice reallocation.
const maxEvents = 50

// applyEventsFn is used for testing
type applyEventsFn func(events []*store.Event)

// watchChanges watches for changes on a channel and
// publishes a batch of changes via applyEvents.
// watchChanges is started in a goroutine.
func watchChanges(wch <-chan store.Event, applyEvents applyEventsFn) {
	// consume changes and apply them to data indefinitely
	var timeChan <-chan time.Time
	var timer *time.Timer
	events := make([]*store.Event, 0, maxEvents)

	for {
		select {
		case ev := <-wch:
			if len(events) == 0 {
				timer = time.NewTimer(watchFlushDuration)
				timeChan = timer.C
			}
			events = append(events, &ev)
		case <-timeChan:
			timer.Stop()
			timeChan = nil
			log.Infof("Publishing %d events", len(events))
			applyEvents(events)
			events = events[:0]
		}
	}
}
