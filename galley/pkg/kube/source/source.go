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

package source

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"

	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/converter"
	kube_meta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("kube", "kube-specific debugging", 0)

// source is an implementation of runtime.Source.
type sourceImpl struct {
	cfg    *converter.Config
	ifaces kube.Interfaces
	ch     chan resource.Event

	listeners []*listener
}

var _ runtime.Source = &sourceImpl{}

// New returns a Kubernetes implementation of runtime.Source.
func New(k kube.Interfaces, resyncPeriod time.Duration, cfg *converter.Config) (runtime.Source, error) {
	return newSource(k, resyncPeriod, cfg, kube_meta.Types.All())
}

// VerifyCRDPresence verifies that all expected CRDs are registered
// with the k8s kube-apiserver.
func VerifyCRDPresence(k kube.Interfaces) error {
	cs, err := k.APIExtensionsClientset()
	if err != nil {
		return err
	}
	return verifyCRDPresence(cs, kube_meta.Types.All())
}

var (
	crdPresencePollInterval = 500 * time.Millisecond
	crdPresensePollTimeout  = time.Minute
)

func verifyCRDPresence(cs clientset.Interface, specs []kube.ResourceSpec) error {
	crdToFind := make(map[string]struct{})
	for _, spec := range specs {
		name := fmt.Sprintf("%s.%s", spec.Plural, spec.Group)
		crdToFind[name] = struct{}{}
	}

	err := wait.Poll(crdPresencePollInterval, crdPresensePollTimeout, func() (bool, error) {
		var errs error
	nextCRD:
		for name := range crdToFind {
			crd, err := cs.ApiextensionsV1beta1().CustomResourceDefinitions().Get(name, v1.GetOptions{})
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("could not find %v: %v", name, err))
				continue nextCRD
			}

			for _, cond := range crd.Status.Conditions {
				switch cond.Type {
				case apiextensionsv1beta1.Established:
					if cond.Status == apiextensionsv1beta1.ConditionTrue {
						log.Infof("Found CRD %q", name)
						delete(crdToFind, name)
						continue nextCRD
					}
				case apiextensionsv1beta1.NamesAccepted:
					if cond.Status == apiextensionsv1beta1.ConditionFalse {
						log.Warnf("CRD name conflict: %v", cond.Reason)
						continue nextCRD
					}
				}
			}
		}
		if len(crdToFind) == 0 {
			log.Infof("Discovered all supported CRD (# = %v)", len(specs))
			return true, nil
		}
		// entire search failed
		if errs != nil {
			return true, errs
		}
		// check again next poll
		return false, nil
	})
	return err
}

func newSource(k kube.Interfaces, resyncPeriod time.Duration, cfg *converter.Config, specs []kube.ResourceSpec) (runtime.Source, error) {
	s := &sourceImpl{
		cfg:    cfg,
		ifaces: k,
	}

	sort.Slice(specs, func(i, j int) bool {
		return strings.Compare(specs[i].CanonicalResourceName(), specs[j].CanonicalResourceName()) < 0
	})

	scope.Infof("Registering the following resources:")
	for i, spec := range specs {
		scope.Infof("[%d]", i)
		scope.Infof("  Source:    %s", spec.CanonicalResourceName())
		scope.Infof("  Type URL:  %s", spec.Target.TypeURL)

		l, err := newListener(k, resyncPeriod, spec, s.process)
		if err != nil {
			scope.Errorf("Error registering listener: %v", err)
			return nil, err
		}

		s.listeners = append(s.listeners, l)
	}

	return s, nil
}

// Start implements runtime.Source
func (s *sourceImpl) Start() (chan resource.Event, error) {
	s.ch = make(chan resource.Event, 1024)

	for _, l := range s.listeners {
		l.start()
	}

	// Wait in a background go-routine until all listeners are synced and send a full-sync event.
	go func() {
		for _, l := range s.listeners {
			l.waitForCacheSync()
		}
		s.ch <- resource.Event{Kind: resource.FullSync}
	}()

	return s.ch, nil
}

// Stop implements runtime.Source
func (s *sourceImpl) Stop() {
	for _, a := range s.listeners {
		a.stop()
	}
}

func (s *sourceImpl) process(l *listener, kind resource.EventKind, key resource.FullName, resourceVersion string, u *unstructured.Unstructured) {
	ProcessEvent(s.cfg, l.spec, kind, key, resourceVersion, u, s.ch)
}

// ProcessEvent process the incoming message and convert it to event
func ProcessEvent(cfg *converter.Config, spec kube.ResourceSpec, kind resource.EventKind, key resource.FullName, resourceVersion string,
	u *unstructured.Unstructured, ch chan resource.Event) {

	var event resource.Event

	switch kind {
	case resource.Added, resource.Updated:
		entries, err := spec.Converter(cfg, spec.Target, key, u)
		if err != nil {
			scope.Errorf("Unable to convert unstructured to proto: %s/%s: %v", key, resourceVersion, err)
			recordConverterResult(false, spec.Version, spec.Group, spec.Kind)
			return
		}
		recordConverterResult(true, spec.Version, spec.Group, spec.Kind)

		if len(entries) == 0 {
			scope.Debugf("Did not receive any entries from converter: kind=%v, key=%v, rv=%s", kind, key, resourceVersion)
			return
		}

		event = resource.Event{
			Kind: kind,
		}

		rid := resource.VersionedKey{
			Key: resource.Key{
				TypeURL:  spec.Target.TypeURL,
				FullName: key,
			},
			Version:    resource.Version(resourceVersion),
			CreateTime: entries[0].CreationTime,
		}

		event.Entry = resource.Entry{
			ID:   rid,
			Item: entries[0].Resource,
		}

	case resource.Deleted:
		rid := resource.VersionedKey{
			Key: resource.Key{
				TypeURL:  spec.Target.TypeURL,
				FullName: key,
			},
			Version: resource.Version(resourceVersion),
		}

		event = resource.Event{
			Kind: kind,
			Entry: resource.Entry{
				ID: rid,
			},
		}
	}

	scope.Debugf("Dispatching source event: %v", event)
	ch <- event
}
