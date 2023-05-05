// Copyright Istio Authors
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

package autoregistration

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/workloadentry/internal/health"
	workloadentrystore "istio.io/istio/pilot/pkg/workloadentry/internal/store"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	istiolog "istio.io/pkg/log"
	"istio.io/pkg/monitoring"
)

var log = istiolog.RegisterScope("wle", "wle controller debugging")

func init() {
	monitoring.MustRegister(autoRegistrationSuccess)
	monitoring.MustRegister(autoRegistrationUpdates)
	monitoring.MustRegister(autoRegistrationUnregistrations)
	monitoring.MustRegister(autoRegistrationDeletes)
	monitoring.MustRegister(autoRegistrationErrors)
}

var (
	autoRegistrationSuccess = monitoring.NewSum(
		"auto_registration_success_total",
		"Total number of successful auto registrations.",
	)

	autoRegistrationUpdates = monitoring.NewSum(
		"auto_registration_updates_total",
		"Total number of auto registration updates.",
	)

	autoRegistrationUnregistrations = monitoring.NewSum(
		"auto_registration_unregister_total",
		"Total number of unregistrations.",
	)

	autoRegistrationDeletes = monitoring.NewSum(
		"auto_registration_deletes_total",
		"Total number of auto registration cleaned up by periodic timer.",
	)

	autoRegistrationErrors = monitoring.NewSum(
		"auto_registration_errors_total",
		"Total number of auto registration errors.",
	)
)

const (
	// AutoRegistrationGroupAnnotation on a WorkloadEntry stores the associated WorkloadGroup.
	AutoRegistrationGroupAnnotation = "istio.io/autoRegistrationGroup"
)

// Controller manages lifecycle of those workloads that are using auto-registration.
type Controller struct {
	// TODO move WorkloadEntry related tasks into their own object and give InternalGen a reference.
	// store should either be k8s (for running pilot) or in-memory (for tests). MCP and other config store implementations
	// do not support writing. We only use it here for reading WorkloadEntry/WorkloadGroup.
	store model.ConfigStoreController

	wleStore *workloadentrystore.Controller
}

// NewController returns a new Controller instance.
func NewController(store model.ConfigStoreController, wleStore *workloadentrystore.Controller) *Controller {
	return &Controller{
		store:    store,
		wleStore: wleStore,
	}
}

func IsApplicableTo(proxy *model.Proxy) bool {
	return features.WorkloadEntryAutoRegistration && proxy.Metadata.AutoRegisterGroup != ""
}

// OnWorkloadConnect creates or updates a WorkloadEntry of a workload that is using
// auto-registration.
func (c *Controller) OnWorkloadConnect(proxy *model.Proxy, conTime time.Time) error {
	entryName := proxy.AutoregisteredWorkloadEntryName
	wle := c.wleStore.Get(entryName, proxy.Metadata.Namespace)
	if wle != nil {
		changed, err := c.wleStore.ChangeStateToConnected(entryName, proxy.Metadata.Namespace, conTime)
		if err != nil {
			autoRegistrationErrors.Increment()
			return err
		}
		if !changed {
			return nil
		}
		autoRegistrationUpdates.Increment()
		log.Infof("updated auto-registered WorkloadEntry %s/%s", proxy.Metadata.Namespace, entryName)
		return nil
	}

	// No WorkloadEntry, create one using fields from the associated WorkloadGroup
	groupCfg := c.store.Get(gvk.WorkloadGroup, proxy.Metadata.AutoRegisterGroup, proxy.Metadata.Namespace)
	if groupCfg == nil {
		autoRegistrationErrors.Increment()
		return grpcstatus.Errorf(codes.FailedPrecondition, "auto-registration WorkloadEntry of %v failed: cannot find WorkloadGroup %s/%s",
			proxy.ID, proxy.Metadata.Namespace, proxy.Metadata.AutoRegisterGroup)
	}
	entry := WorkloadEntryFromGroup(entryName, proxy, groupCfg)
	_, err := c.wleStore.Create(*entry, conTime)
	if err != nil {
		autoRegistrationErrors.Increment()
		return fmt.Errorf("auto-registration WorkloadEntry of %v failed: error creating WorkloadEntry: %v", proxy.ID, err)
	}
	hcMessage := ""
	if health.IsElegibleForHealthStatusUpdates(entry) {
		hcMessage = " with health checking enabled"
	}
	autoRegistrationSuccess.Increment()
	log.Infof("auto-registered WorkloadEntry %s/%s%s", proxy.Metadata.Namespace, entryName, hcMessage)
	return nil
}

// OnWorkloadDisconnect handles workload disconnect.
func (c *Controller) OnWorkloadDisconnect() *Controller {
	autoRegistrationUnregistrations.Increment()
	return c
}

// GetCleanupGracePeriod implements WorkloadEntryCleaner.
func (c *Controller) GetCleanupGracePeriod() time.Duration {
	return features.WorkloadEntryCleanupGracePeriod
}

// ShouldCleanup implements WorkloadEntryCleaner.
func (c *Controller) ShouldCleanup(wle *config.Config, maxConnectionAge time.Duration) bool {
	return IsAutoRegisteredWorkloadEntry(wle) && workloadentrystore.IsExpired(wle, maxConnectionAge, c.GetCleanupGracePeriod())
}

// Cleanup removes WorkloadEntry that was created automatically for a workload
// that is using auto-registration.
func (c *Controller) Cleanup(wle *config.Config, periodic bool) {
	if !IsAutoRegisteredWorkloadEntry(wle) {
		return
	}
	err := c.wleStore.Delete(wle)
	if err != nil {
		log.Warnf("failed cleaning up auto-registered WorkloadEntry %s/%s: %v", wle.Namespace, wle.Name, err)
		autoRegistrationErrors.Increment()
		return
	}
	autoRegistrationDeletes.Increment()
	log.Infof("cleaned up auto-registered WorkloadEntry %s/%s periodic:%v", wle.Namespace, wle.Name, periodic)
}

func IsAutoRegisteredWorkloadEntry(wle *config.Config) bool {
	return wle != nil && wle.Annotations[AutoRegistrationGroupAnnotation] != ""
}

func GenerateWorkloadEntryName(proxy *model.Proxy) string {
	if proxy.Metadata.AutoRegisterGroup == "" {
		return ""
	}
	if len(proxy.IPAddresses) == 0 {
		log.Errorf("auto-registration of %v failed: missing IP addresses", proxy.ID)
		return ""
	}
	if len(proxy.Metadata.Namespace) == 0 {
		log.Errorf("auto-registration of %v failed: missing namespace", proxy.ID)
		return ""
	}
	p := []string{proxy.Metadata.AutoRegisterGroup, sanitizeIP(proxy.IPAddresses[0])}
	if proxy.Metadata.Network != "" {
		p = append(p, string(proxy.Metadata.Network))
	}

	name := strings.Join(p, "-")
	if len(name) > 253 {
		name = name[len(name)-253:]
		log.Warnf("generated WorkloadEntry name is too long, consider making the WorkloadGroup name shorter. Shortening from beginning to: %s", name)
	}
	return name
}

// sanitizeIP ensures an IP address (IPv6) can be used in Kubernetes resource name
func sanitizeIP(s string) string {
	return strings.ReplaceAll(s, ":", "-")
}

var WorkloadGroupIsController = true

func WorkloadEntryFromGroup(name string, proxy *model.Proxy, groupCfg *config.Config) *config.Config {
	group := groupCfg.Spec.(*v1alpha3.WorkloadGroup)
	entry := group.Template.DeepCopy()
	entry.Address = proxy.IPAddresses[0]
	// TODO move labels out of entry
	// node metadata > WorkloadGroup.Metadata > WorkloadGroup.Template
	if group.Metadata != nil && group.Metadata.Labels != nil {
		entry.Labels = mergeLabels(entry.Labels, group.Metadata.Labels)
	}
	// Explicitly do not use proxy.Labels, as it is only initialized *after* we register the workload,
	// and it would be circular, as it will set the labels based on the WorkloadEntry -- but we are creating
	// the workload entry.
	if proxy.Metadata.Labels != nil {
		entry.Labels = mergeLabels(entry.Labels, proxy.Metadata.Labels)
	}

	annotations := map[string]string{AutoRegistrationGroupAnnotation: groupCfg.Name}
	if group.Metadata != nil && group.Metadata.Annotations != nil {
		annotations = mergeLabels(annotations, group.Metadata.Annotations)
	}

	if proxy.Metadata.Network != "" {
		entry.Network = string(proxy.Metadata.Network)
	}
	// proxy.Locality is unset when auto registration takes place, because its
	// state is not fully initialized. Therefore, we check the bootstrap node.
	if proxy.XdsNode.Locality != nil {
		entry.Locality = util.LocalityToString(proxy.XdsNode.Locality)
	}
	if proxy.Metadata.ProxyConfig != nil && proxy.Metadata.ProxyConfig.ReadinessProbe != nil {
		annotations[status.WorkloadEntryHealthCheckAnnotation] = "true"
	}
	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadEntry,
			Name:             name,
			Namespace:        proxy.Metadata.Namespace,
			Labels:           entry.Labels,
			Annotations:      annotations,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: groupCfg.GroupVersionKind.GroupVersion(),
				Kind:       groupCfg.GroupVersionKind.Kind,
				Name:       groupCfg.Name,
				UID:        kubetypes.UID(groupCfg.UID),
				Controller: &WorkloadGroupIsController,
			}},
		},
		Spec: entry,
		// TODO status fields used for garbage collection
		Status: nil,
	}
}

func mergeLabels(labels ...map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(labels)*len(labels[0]))
	for _, lm := range labels {
		for k, v := range lm {
			out[k] = v
		}
	}
	return out
}
