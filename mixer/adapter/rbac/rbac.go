// Copyright 2018 Istio Authors.
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

// nolint: lll
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -a mixer/adapter/rbac/config/config.proto -x "-n rbac -t authorization"

// Package rbac provides Role Based Access Control (RBAC) for services in Istio mesh.
// Seting up RBAC handler is trivial. The runtime input to RBAC handler should be an instance of
// "authorization" template.
//
// The RBAC policies are specified in ServiceRole and ServiceRoleBinding CRD objects.
// You can define a ServiceRole that contains a set of permissions for service/method level
// access. You can then assign a ServiceRole to a set of subjects using ServiceRoleBinding specification.
// ServiceRole and the corresponding ServiceRoleBindings should be in the same namespace.
// Please see "istio.io/istio/mixer/testdata/config/rbac.yaml" for an example of RBAC handler, plus ServiceRole
// ServiceRoleBinding specifications.
package rbac

import (
	"context"
	"net/url"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/gogo/protobuf/proto"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/mixer/adapter/rbac/config"
	"istio.io/istio/mixer/pkg/adapter"
	mixerconfig "istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/authorization"
)

type (
	builder struct {
		adapterConfig *config.Params
	}
	handler struct {
		rbac          authorizer
		env           adapter.Env
		cacheDuration time.Duration
		store         store.Store
		closing       chan bool
		done          chan bool
	}
)

const (
	// API group and version string for the RBAC CRDs.
	apiGroup   = "rbac.istio.io"
	apiVersion = "v1alpha1"

	// ServiceRoleKind defines the config kind name of ServiceRole.
	serviceRoleKind = "ServiceRole"

	// ServiceRoleBindingKind defines the config kind name of ServiceRoleBinding.
	serviceRoleBindingKind = "ServiceRoleBinding"
)

///////////////// Configuration-time Methods ///////////////

func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adapterConfig = cfg.(*config.Params)
}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	ac := b.adapterConfig

	if _, err := url.Parse(ac.ConfigStoreUrl); err != nil {
		ce = ce.Append("configStoreUrl", err)
	}

	if ac.CacheDuration < 0 {
		ce = ce.Appendf("cachingDuration", "caching interval must be >= 0, it is %v", ac.CacheDuration)
	}
	return
}

func (b *builder) SetAuthorizationTypes(types map[string]*authorization.Type) {}

func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	reg := store.NewRegistry(mixerconfig.StoreInventory()...)
	groupVersion := &schema.GroupVersion{Group: apiGroup, Version: apiVersion}
	s, err := reg.NewStore(b.adapterConfig.ConfigStoreUrl, groupVersion, nil)
	if err != nil {
		return nil, env.Logger().Errorf("Unable to connect to the configuration server: %v", err)
	}
	r := &ConfigStore{}
	h := &handler{
		rbac:          r,
		env:           env,
		cacheDuration: b.adapterConfig.CacheDuration,
		store:         s,
		closing:       make(chan bool),
		done:          make(chan bool),
	}
	if err = h.startController(s); err != nil {
		return nil, env.Logger().Errorf("Unable to start controller: %v", err)
	}

	return h, nil
}

// maxEvents is the likely maximum number of events
// we can expect in a second. It is used to avoid slice reallocation.
const maxEvents = 50

var watchFlushDuration = time.Second

// startController creates a controller from the given params.
func (h *handler) startController(s store.Store) error {
	data, watchChan, err := startWatch(s)
	if err != nil {
		return h.env.Logger().Errorf("Error while starting watching CRDs: %v", err)
	}

	c := &controller{
		configState: data,
		rbacStore:   h.rbac.(*ConfigStore),
	}

	c.processRBACRoles(h.env)
	// Start a goroutine to watch for changes on a channel and
	// publishes a batch of changes via applyEvents.
	h.env.ScheduleDaemon(func() {
		// consume changes and apply them to data.
		var timeChan <-chan time.Time
		var timer *time.Timer
		events := make([]*store.Event, 0, maxEvents)

		for {
			select {
			case ev := <-watchChan:
				if len(events) == 0 {
					timer = time.NewTimer(watchFlushDuration)
					timeChan = timer.C
				}
				events = append(events, &ev)
			case <-timeChan:
				timer.Stop()
				timeChan = nil
				h.env.Logger().Infof("Publishing %d events", len(events))
				c.applyEvents(events, h.env)
				events = events[:0]
			case <-h.closing:
				if timer != nil {
					timer.Stop()
					timeChan = nil
				}
				h.done <- true
				return
			}
		}
	})
	return nil
}

// startWatch registers with store, initiates a watch, and returns the current config state.
func startWatch(s store.Store) (map[store.Key]*store.Resource, <-chan store.Event, error) {
	kindMap := make(map[string]proto.Message)
	kindMap[serviceRoleKind] = &rbacproto.ServiceRole{}
	kindMap[serviceRoleBindingKind] = &rbacproto.ServiceRoleBinding{}

	if err := s.Init(kindMap); err != nil {
		return nil, nil, err
	}
	// create channel before listing.
	watchChan, err := s.Watch()
	if err != nil {
		return nil, nil, err
	}
	return s.List(), watchChan, nil
}

////////////////// Request-time Methods //////////////////////////
// authorization.Handler#HandleAuthorization
func (h *handler) HandleAuthorization(ctx context.Context, inst *authorization.Instance) (adapter.CheckResult, error) {
	s := status.OK
	result, err := h.rbac.CheckPermission(inst, h.env.Logger())
	if !result || err != nil {
		s = status.WithPermissionDenied("RBAC: permission denied.")
	}
	return adapter.CheckResult{
		Status:        s,
		ValidDuration: h.cacheDuration,
		ValidUseCount: 1000000000,
	}, nil
}

// adapter.Handler#Close
func (h *handler) Close() error {
	h.closing <- true
	close(h.closing)

	<-h.done
	h.store.Stop()
	return nil
}

////////////////// Bootstrap //////////////////////////

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "rbac",
		Impl:        "istio.io/istio/mixer/adapter/rbac",
		Description: "Role Based Access Control for Istio services",
		SupportedTemplates: []string{
			authorization.TemplateName,
		},
		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{
			ConfigStoreUrl: "k8s://",
			CacheDuration:  60 * time.Second,
		},
	}
}
