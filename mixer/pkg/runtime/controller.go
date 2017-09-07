// Copyright 2017 Istio Authors
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

package runtime

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang/glog"

	"istio.io/mixer/pkg/adapter"
	adptTmpl "istio.io/mixer/pkg/adapter/template"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/config/store"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/template"
)

// Controller is responsible for watching configuration using the Store2 API.
// Controller produces a resolver and installs it in the dispatcher.
// Controller consumes potentially inconsistent configuration state from the config store
// and produces a consistent snapshot.
// Controller must not panic on configuration problems, it should issues a warning and continue.
type Controller struct {
	// Static information
	adapterInfo            map[string]*adapter.BuilderInfo // maps adapter shortName to BuilderInfo.
	templateInfo           map[string]template.Info        // maps template name to BuilderInfo.
	eval                   expr.Evaluator                  // Used to infer types. Used by resolver and dispatcher.
	identityAttribute      string                          // used by resolver
	defaultConfigNamespace string                          // used by resolver

	// configState is the current (potentially inconsistent) view of config.
	// It receives updates from the underlying config store.
	configState map[store.Key]*store.Resource

	// currently deployed resolver
	resolver *resolver

	// table is the handler state currently in use.
	table map[string]*HandlerEntry

	// dispatcher is notified of changes.
	dispatcher ResolverChangeListener

	// handlerGoRoutinePool is the goroutine pool used by handlers.
	handlerGoRoutinePool *pool.GoroutinePool

	// nextResolverID is the resolver ID to be assigned to the next resolver.
	// It is incremented every call.
	nextResolverID int

	// changedKinds is updated by applyEvents to indicate the kinds
	// that have changed in the current batch of changes.
	// It is reset on every config change.
	changedKinds map[string]bool

	// df is the cached version of descriptorFinder.
	// It is recreated when attributes change.
	df expr.AttributeDescriptorFinder

	// Fields below are used for testing an debugging.

	// createHandlerFactory for testing.
	createHandlerFactory factoryCreatorFunc

	// number of rules in the resolver.
	nrules int
}

// RulesKind defines the config kind name of mixer rules.
const RulesKind = "rule"

// AttributeManifestKind define the config kind name of attribute manifests.
const AttributeManifestKind = "attributemanifest"

// ResolverChangeListener is notified when a new resolver is created due to config change.
type ResolverChangeListener interface {
	ChangeResolver(rt Resolver)
}

// VocabularyChangeListener is notified when attribute vocabulary changes.
type VocabularyChangeListener interface {
	ChangeVocabulary(finder expr.AttributeDescriptorFinder)
}

// factoryCreatorFunc creates a handler factory. It is used for testing.
type factoryCreatorFunc func(templateInfo map[string]template.Info, expr expr.TypeChecker,
	df expr.AttributeDescriptorFinder, builderInfo map[string]*adapter.BuilderInfo) HandlerFactory

// applyEventsFn is used for testing
type applyEventsFn func(events []*store.Event)

// publishSnapShot converts the currently available configState into a resolver.
// The config may be in an inconsistent state, however it *must* be converted into a consistent resolver.
// The previous handler table enables handler cleanup and reuse.
// This code is single threaded, it only runs on a config change control loop.
func (c *Controller) publishSnapShot() {
	// current view of attributes
	// attribute manifests are used by type inference during handler creation.
	attributes := c.processAttributeManifests()

	if cl, ok := c.eval.(VocabularyChangeListener); ok {
		cl.ChangeVocabulary(attributes)
	}

	// current consistent view of handler configuration
	// keyed by Name.Kind.NameSpace
	handlerConfig := c.validHandlerConfigs()

	// current consistent view of the Instance configuration
	// keyed by Name.Kind.NameSpace
	instanceConfig := c.validInstanceConfigs()

	// new handler factory is created for every config change.
	hb := c.createHandlerFactory(c.templateInfo, c.eval, attributes, c.adapterInfo)

	// new handler table is created for every config change. It uses handler factory
	// to create new handlers.
	ht := newHandlerTable(instanceConfig, handlerConfig,
		func(handler *cpb.Handler, instances []*cpb.Instance) (adapter.Handler, error) {
			return hb.Build(handler, instances, newEnv(handler.Name, c.handlerGoRoutinePool))
		},
	)

	// current consistent view of the rules keyed by Namespace and then Name.
	// ht (handlerTable) keeps track of handler-instance association.
	ruleConfig := c.processRules(handlerConfig, instanceConfig, ht)

	// Initialize handlers that are used in the configuration.
	// Some handlers may not initialize due to errors.
	ht.Initialize(c.table)

	// Combine rules with the handler table.
	// Actions referring to handlers in error are logged and purged.
	resolvedRules, nrules := generateResolvedRules(ruleConfig, ht.table)

	// Create new resolver and cleanup the old resolver.
	c.nextResolverID++
	resolver := newResolver(c.eval, c.identityAttribute, c.defaultConfigNamespace, resolvedRules, c.nextResolverID)
	c.dispatcher.ChangeResolver(resolver)

	// copy old for deletion.
	oldTable := c.table
	oldResolver := c.resolver
	oldNrules := c.nrules

	// set new
	c.table = ht.table
	c.resolver = resolver
	c.nrules = nrules

	glog.Infof("Published snapshot[%d] with %d rules, %d handlers, previously %d rules", resolver.id, nrules, len(c.table), oldNrules)

	// synchronous call to cleanup.
	err := cleanupResolver(oldResolver, oldTable, maxCleanupDuration)
	if err != nil {
		glog.Warningf("Unable to perform cleanup: %v", err)
	}
}

// maxCleanupDuration is the maximum amount of time cleanup operation will wait
// before resolver ref count does to 0. It will return after this duration without
// calling Close() on handlers.
var maxCleanupDuration = 10 * time.Second

var watchFlushDuration = time.Second

// maxEvents is the likely maximum number of events
// we can expect in a second. It is used to avoid slice reallocation.
const maxEvents = 50

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
			glog.Infof("Publishing %d events", len(events))
			applyEvents(events)
			events = events[:0]
		}
	}
}

// applyEvents applies given events to config state and then publishes a snapshot.
func (c *Controller) applyEvents(events []*store.Event) {
	ck := make(map[string]bool)
	for _, ev := range events {
		ck[ev.Kind] = true
		switch ev.Type {
		case store.Update:
			c.configState[ev.Key] = ev.Value
		case store.Delete:
			delete(c.configState, ev.Key)
		}
	}
	c.changedKinds = ck
	c.publishSnapShot()
}

// validInstanceConfigs returns instanceConfigs from the configState that
// point to valid templates.
func (c *Controller) validInstanceConfigs() map[string]*cpb.Instance {
	instanceConfig := make(map[string]*cpb.Instance)

	// first pass get all the validated instance and handler references
	for k, cfg := range c.configState {
		if _, found := c.templateInfo[k.Kind]; !found {
			continue
		}
		// instances use their fully qualified names.
		instanceConfig[k.String()] = &cpb.Instance{
			Name:     k.String(),
			Template: k.Kind,
			Params:   cfg.Spec,
		}
	}
	return instanceConfig
}

// validHandlerConfigs returns handlerConfigs from the configState that
// point to valid adapters.
func (c *Controller) validHandlerConfigs() map[string]*cpb.Handler {
	handlerConfig := make(map[string]*cpb.Handler)
	for k, cfg := range c.configState {
		if _, found := c.adapterInfo[k.Kind]; !found {
			continue
		}
		// handlers use their fully qualified names.
		handlerConfig[k.String()] = &cpb.Handler{
			Name:    k.String(),
			Adapter: k.Kind,
			Params:  cfg.Spec,
		}
	}
	if glog.V(3) {
		glog.Infof("handler = %v", handlerConfig)
	}
	return handlerConfig
}

// processAttributeManifests loads attribute manifests to produce an AttributeDescriptorFinder.
// attribute manifests are not expected to change often.
func (c *Controller) processAttributeManifests() expr.AttributeDescriptorFinder {
	if !c.changedKinds[AttributeManifestKind] && c.df != nil {
		return c.df
	}
	attrs := make(map[string]*cpb.AttributeManifest_AttributeInfo)
	for k, obj := range c.configState {
		if k.Kind != AttributeManifestKind {
			continue
		}
		cfg := obj.Spec
		for an, at := range cfg.(*cpb.AttributeManifest).Attributes {
			attrs[an] = at
		}
	}
	if glog.V(2) {
		glog.Infof("%d known attributes", len(attrs))
	}
	c.df = &attributeFinder{attrs: attrs}
	return c.df
}

// attributeFinder exposes expr.AttributeDescriptorFinder
type attributeFinder struct {
	attrs map[string]*cpb.AttributeManifest_AttributeInfo
}

// GetAttribute finds an attribute by name.
// This function is only called when a new handler is instantiated.
func (a attributeFinder) GetAttribute(name string) *cpb.AttributeManifest_AttributeInfo {
	return a.attrs[name]
}

// rulesByName is rules indexed by name.
type rulesByName map[string]*Rule

// rulesMapByNamespace is rulesByName indexed by namespace.
type rulesMapByNamespace map[string]rulesByName

// rulesListByNamespace is the type needed by resolver.
type rulesListByNamespace map[string][]*Rule

// convertToRuntimeRules converts internal rules to the format that resolver needs.
func convertToRuntimeRules(ruleConfig rulesMapByNamespace) (rulesListByNamespace, int) {
	// convert rules
	nrules := 0
	rules := make(rulesListByNamespace)
	for ns, nsmap := range ruleConfig {
		rulesArr := make([]*Rule, 0, len(nsmap))
		for _, rule := range nsmap {
			rulesArr = append(rulesArr, rule)
		}
		rules[ns] = rulesArr
		nrules += len(rulesArr)
	}
	return rules, nrules
}

const (
	istioProtocol = "istio-protocol"
)

// resourceType maps labels to rule types.
func resourceType(labels map[string]string) ResourceType {
	ip := labels[istioProtocol]
	rt := defaultResourcetype()
	if ip == "tcp" {
		rt.protocol = protocolTCP
	}
	return rt
}

// processRules builds the current consistent view of the rules keyed by Namespace and then Name.
// ht (handlerTable) keeps track of handler-instance association.
func (c *Controller) processRules(handlerConfig map[string]*cpb.Handler,
	instanceConfig map[string]*cpb.Instance, ht *handlerTable) rulesMapByNamespace {
	// current consistent view of the rules
	// keyed by Namespace and then Name.
	ruleConfig := make(rulesMapByNamespace)

	// check rules and ensure only good handlers and instances are used.
	// record handler - instance associations
	for k, obj := range c.configState {
		if k.Kind != RulesKind {
			continue
		}

		cfg := obj.Spec
		rulec := cfg.(*cpb.Rule)
		rule := &Rule{
			selector: rulec.Selector,
			name:     k.Name,
			rtype:    resourceType(obj.Metadata.Labels),
		}
		acts := c.processActions(rulec.Actions, handlerConfig, instanceConfig, ht, k.Namespace)

		ruleActions := make(map[adptTmpl.TemplateVariety][]*Action)
		for vr, amap := range acts {
			for _, cf := range amap {
				ruleActions[vr] = append(ruleActions[vr], cf)
			}
		}

		rule.actions = ruleActions
		rn := ruleConfig[k.Namespace]
		if rn == nil {
			rn = make(map[string]*Rule)
			ruleConfig[k.Namespace] = rn
		}
		rn[k.Name] = rule
	}

	return ruleConfig
}

var cleanupSleepTime = 500 * time.Millisecond

// isFQN returns true if the name is fully qualified.
// every resource name is defined by Key.String()
// shortname.kind.namespace
func isFQN(name string) bool {
	return len(strings.Split(name, ".")) == 3
}

// canonicalizeHandlerNames ensures that all handler names are fully qualified.
func canonicalizeHandlerNames(acts []*cpb.Action, namespace string) []*cpb.Action {
	for _, ic := range acts {
		if !isFQN(ic.Handler) {
			ic.Handler = ic.Handler + "." + namespace
		}
	}
	return acts
}

// canonicalizeInstanceNames ensures that all instance names are fully qualified.
func canonicalizeInstanceNames(instances []string, namespace string) []string {
	for i := range instances {
		if !isFQN(instances[i]) {
			instances[i] = instances[i] + "." + namespace
		}
	}
	return instances
}

// processActions prunes actions that lack referential integrity and associate instances with
// handlers that are later used to create new handlers.
func (c *Controller) processActions(acts []*cpb.Action, handlerConfig map[string]*cpb.Handler,
	instanceConfig map[string]*cpb.Instance, ht *handlerTable, namespace string) map[adptTmpl.TemplateVariety]map[string]*Action {

	actions := make(map[adptTmpl.TemplateVariety]map[string]*Action)

	for _, ic := range canonicalizeHandlerNames(acts, namespace) {
		var hc *cpb.Handler
		if hc = handlerConfig[ic.Handler]; hc == nil {
			if glog.V(3) {
				glog.Warningf("ConfigWarning unknown handler: %s", ic.Handler)
			}
			continue
		}

		for _, instName := range canonicalizeInstanceNames(ic.Instances, namespace) {
			inst := instanceConfig[instName]
			if inst == nil {
				if glog.V(3) {
					glog.Warningf("ConfigWarning unknown instance: %s", instName)
				}
				continue
			}

			ht.Associate(ic.Handler, instName)

			ti := c.templateInfo[inst.Template]
			vAction := actions[ti.Variety]
			if vAction == nil {
				vAction = make(map[string]*Action)
				actions[ti.Variety] = vAction
			}

			// runtime.Action requires that the instanceConfig
			// must belong to the same template and handler.
			templateHandlerKey := ic.Handler + "/" + inst.Template

			act := vAction[templateHandlerKey]
			if act == nil {
				act = &Action{
					processor:   &ti,
					handlerName: ic.Handler,
					adapterName: hc.Adapter,
				}
				vAction[templateHandlerKey] = act
			}
			act.instanceConfig = append(act.instanceConfig, inst)
		}
	}
	return actions
}

// generateResolvedRules sets handler references in rulesConfig.
// It reject actions from rulesConfig whose handler could not be initialized.
func generateResolvedRules(ruleConfig rulesMapByNamespace, handlerTable map[string]*HandlerEntry) (rulesListByNamespace, int) {
	// map by namespace
	for ns, nsmap := range ruleConfig {
		// map by rule name
		for rn, rule := range nsmap {
			// map by template variety
			for vr, vact := range rule.actions {
				newvact := vact[:0]
				for _, act := range vact {
					he := handlerTable[act.handlerName]
					if he == nil {
						glog.Warningf("Internal error: Handler %s could not be found", act.handlerName)
						continue
					}
					if he.Handler == nil {
						glog.Warningf("Filtering action from rule %s/%s. Handler %s could not be initialized due to %s.", ns, rn, act.handlerName, he.HandlerCreateError)
						continue
					}
					act.handler = he.Handler
					newvact = append(newvact, act)
				}
				if len(newvact) > 0 {
					rule.actions[vr] = newvact
				} else {
					delete(rule.actions, vr)
				}
			}
			if len(rule.actions) == 0 {
				glog.Warningf("Purging rule %v with no actions", rn)
				delete(nsmap, rn)
			}
		}
	}

	// create rules that are in the format that resolver needs.
	return convertToRuntimeRules(ruleConfig)
}

// cleanupResolver cleans up handler table in the resolver
// after the resolver is no longer in use.
func cleanupResolver(r *resolver, table map[string]*HandlerEntry, timeout time.Duration) error {
	start := time.Now()
	for {
		rc := atomic.LoadInt32(&r.refCount)
		if rc > 0 {
			if time.Since(start) > timeout {
				return fmt.Errorf("unable to cleanup resolver in %v time. %d requests remain", timeout, rc)
			}
			if glog.V(2) {
				glog.Infof("Waiting for resolver %d to finish %d remaining requests", r.id, rc)
			}
			time.Sleep(cleanupSleepTime)
			continue
		}
		if glog.V(2) {
			glog.Infof("cleanupResolver[%d] handler table has %d entries", r.id, len(table))
		}
		for _, he := range table {
			if he.closeOnCleanup && he.Handler != nil {
				msg := fmt.Sprintf("closing %s/%v", he.Name, he.Handler)
				err := he.Handler.Close()
				if err != nil {
					glog.Warningf("Error "+msg+": %s", err)
				} else {
					glog.Info(msg)
				}
			}
		}
		return nil
	}
}
