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

package rbac

import (
	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
)

type controller struct {
	// Current view of config. It receives updates from the underlying config store.
	configState map[store.Key]*store.Resource

	// Pointer to RBAC config store instance.
	rbacStore *ConfigStore
}

// applyEvents applies given events to config state and then publishes a snapshot.
func (c *controller) applyEvents(events []*store.Event, env adapter.Env) {
	for _, ev := range events {
		switch ev.Type {
		case store.Update:
			c.configState[ev.Key] = ev.Value
		case store.Delete:
			delete(c.configState, ev.Key)
		}
	}
	c.processRBACRoles(env)
}

// processRBACRoles processes ServiceRole and ServiceRoleBinding CRDs and save them to
// RBAC store data structure.
func (c *controller) processRBACRoles(env adapter.Env) {
	roles := make(RolesMapByNamespace)

	for k, obj := range c.configState {
		if k.Kind == serviceRoleKind {
			cfg := obj.Spec
			roleSpec := cfg.(*rbacproto.ServiceRole)
			err := roles.AddServiceRole(k.Name, k.Namespace, roleSpec)
			if err != nil {
				_ = env.Logger().Errorf("Failed to add ServiceRole: %v", err)
				continue
			}

			env.Logger().Infof("Role namespace: %s, name: %s, spec: %v", k.Namespace, k.Name, roleSpec)
		}
	}

	for k, obj := range c.configState {
		if k.Kind == serviceRoleBindingKind {
			cfg := obj.Spec
			bindingSpec := cfg.(*rbacproto.ServiceRoleBinding)
			roleName := bindingSpec.GetRoleRef().GetName()

			err := roles.AddServiceRoleBinding(k.Name, k.Namespace, bindingSpec)
			if err != nil {
				_ = env.Logger().Errorf("Failed to add ServiceRoleBinding: %v", err)
				continue
			}

			env.Logger().Infof("RoleBinding: %s for role %s, Spec: %v", k.Name, roleName, bindingSpec)
		}
	}

	c.rbacStore.changeRoles(roles)
}
