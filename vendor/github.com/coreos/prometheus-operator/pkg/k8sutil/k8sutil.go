// Copyright 2016 The prometheus-operator Authors
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

package k8sutil

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	crdutils "github.com/ant31/crd-validation/pkg"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	version "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

var invalidDNS1123Characters = regexp.MustCompile("[^-a-z0-9]+")

// CustomResourceDefinitionTypeMeta set the default kind/apiversion of CRD
var CustomResourceDefinitionTypeMeta metav1.TypeMeta = metav1.TypeMeta{
	Kind:       "CustomResourceDefinition",
	APIVersion: "apiextensions.k8s.io/v1beta1",
}

// WaitForCRDReady waits for a custom resource definition to be available for use.
func WaitForCRDReady(listFunc func(opts metav1.ListOptions) (runtime.Object, error)) error {
	err := wait.Poll(3*time.Second, 10*time.Minute, func() (bool, error) {
		_, err := listFunc(metav1.ListOptions{})
		if err != nil {
			if se, ok := err.(*apierrors.StatusError); ok {
				if se.Status().Code == http.StatusNotFound {
					return false, nil
				}
			}
			return false, errors.Wrap(err, "failed to list CRD")
		}
		return true, nil
	})

	return errors.Wrap(err, fmt.Sprintf("timed out waiting for Custom Resource"))
}

// PodRunningAndReady returns whether a pod is running and each container has
// passed it's ready state.
func PodRunningAndReady(pod v1.Pod) (bool, error) {
	switch pod.Status.Phase {
	case v1.PodFailed, v1.PodSucceeded:
		return false, fmt.Errorf("pod completed")
	case v1.PodRunning:
		for _, cond := range pod.Status.Conditions {
			if cond.Type != v1.PodReady {
				continue
			}
			return cond.Status == v1.ConditionTrue, nil
		}
		return false, fmt.Errorf("pod ready condition not found")
	}
	return false, nil
}

func NewClusterConfig(host string, tlsInsecure bool, tlsConfig *rest.TLSClientConfig) (*rest.Config, error) {
	var cfg *rest.Config
	var err error

	if len(host) == 0 {
		if cfg, err = rest.InClusterConfig(); err != nil {
			return nil, err
		}
	} else {
		cfg = &rest.Config{
			Host: host,
		}
		hostURL, err := url.Parse(host)
		if err != nil {
			return nil, fmt.Errorf("error parsing host url %s : %v", host, err)
		}
		if hostURL.Scheme == "https" {
			cfg.TLSClientConfig = *tlsConfig
			cfg.Insecure = tlsInsecure
		}
	}
	cfg.QPS = 100
	cfg.Burst = 100

	return cfg, nil
}

func IsResourceNotFoundError(err error) bool {
	se, ok := err.(*apierrors.StatusError)
	if !ok {
		return false
	}
	if se.Status().Code == http.StatusNotFound && se.Status().Reason == metav1.StatusReasonNotFound {
		return true
	}
	return false
}

func CreateOrUpdateService(sclient clientv1.ServiceInterface, svc *v1.Service) error {
	service, err := sclient.Get(svc.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "retrieving service object failed")
	}

	if apierrors.IsNotFound(err) {
		_, err = sclient.Create(svc)
		if err != nil {
			return errors.Wrap(err, "creating service object failed")
		}
	} else {
		svc.ResourceVersion = service.ResourceVersion
		svc.SetOwnerReferences(mergeOwnerReferences(service.GetOwnerReferences(), svc.GetOwnerReferences()))
		_, err := sclient.Update(svc)
		if err != nil && !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "updating service object failed")
		}
	}

	return nil
}

func CreateOrUpdateEndpoints(eclient clientv1.EndpointsInterface, eps *v1.Endpoints) error {
	endpoints, err := eclient.Get(eps.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "retrieving existing kubelet endpoints object failed")
	}

	if apierrors.IsNotFound(err) {
		_, err = eclient.Create(eps)
		if err != nil {
			return errors.Wrap(err, "creating kubelet endpoints object failed")
		}
	} else {
		eps.ResourceVersion = endpoints.ResourceVersion
		_, err = eclient.Update(eps)
		if err != nil {
			return errors.Wrap(err, "updating kubelet endpoints object failed")
		}
	}

	return nil
}

// GetMinorVersion returns the minor version as an integer
func GetMinorVersion(dclient discovery.DiscoveryInterface) (int, error) {
	v, err := dclient.ServerVersion()
	if err != nil {
		return 0, err
	}

	ver, err := version.NewVersion(v.String())
	if err != nil {
		return 0, err
	}

	return ver.Segments()[1], nil
}

func NewCustomResourceDefinition(crdKind monitoringv1.CrdKind, group string, labels map[string]string, validation bool) *extensionsobj.CustomResourceDefinition {
	return crdutils.NewCustomResourceDefinition(crdutils.Config{
		SpecDefinitionName:    crdKind.SpecName,
		EnableValidation:      validation,
		Labels:                crdutils.Labels{LabelsMap: labels},
		ResourceScope:         string(extensionsobj.NamespaceScoped),
		Group:                 group,
		Kind:                  crdKind.Kind,
		Version:               monitoringv1.Version,
		Plural:                crdKind.Plural,
		GetOpenAPIDefinitions: monitoringv1.GetOpenAPIDefinitions,
	})
}

// SanitizeVolumeName ensures that the given volume name is a valid DNS-1123 label
// accepted by Kubernetes.
func SanitizeVolumeName(name string) string {
	name = strings.ToLower(name)
	name = invalidDNS1123Characters.ReplaceAllString(name, "-")
	if len(name) > validation.DNS1123LabelMaxLength {
		name = name[0:validation.DNS1123LabelMaxLength]
	}
	return strings.Trim(name, "-")
}

func mergeOwnerReferences(old []metav1.OwnerReference, new []metav1.OwnerReference) []metav1.OwnerReference {
	existing := make(map[metav1.OwnerReference]bool)
	for _, ownerRef := range old {
		existing[ownerRef] = true
	}
	for _, ownerRef := range new {
		if _, ok := existing[ownerRef]; !ok {
			old = append(old, ownerRef)
		}
	}
	return old
}
