//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package mock

import (
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1alpha1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1alpha1"
	admissionregistrationv1beta1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	appsv1beta1 "k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	appsv1beta2 "k8s.io/client-go/kubernetes/typed/apps/v1beta2"
	authenticationv1 "k8s.io/client-go/kubernetes/typed/authentication/v1"
	authenticationv1beta1 "k8s.io/client-go/kubernetes/typed/authentication/v1beta1"
	authorizationv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
	authorizationv1beta1 "k8s.io/client-go/kubernetes/typed/authorization/v1beta1"
	autoscalingv1 "k8s.io/client-go/kubernetes/typed/autoscaling/v1"
	autoscalingv2beta1 "k8s.io/client-go/kubernetes/typed/autoscaling/v2beta1"
	batchv1 "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchv1beta1 "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	batchv2alpha1 "k8s.io/client-go/kubernetes/typed/batch/v2alpha1"
	certificatesv1beta1 "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	eventsv1beta1 "k8s.io/client-go/kubernetes/typed/events/v1beta1"
	extensionsv1beta1 "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	networkingv1 "k8s.io/client-go/kubernetes/typed/networking/v1"
	policyv1beta1 "k8s.io/client-go/kubernetes/typed/policy/v1beta1"
	rbacv1 "k8s.io/client-go/kubernetes/typed/rbac/v1"
	rbacv1alpha1 "k8s.io/client-go/kubernetes/typed/rbac/v1alpha1"
	rbacv1beta1 "k8s.io/client-go/kubernetes/typed/rbac/v1beta1"
	schedulingv1alpha1 "k8s.io/client-go/kubernetes/typed/scheduling/v1alpha1"
	settingsv1alpha1 "k8s.io/client-go/kubernetes/typed/settings/v1alpha1"
	storagev1 "k8s.io/client-go/kubernetes/typed/storage/v1"
	storagev1alpha1 "k8s.io/client-go/kubernetes/typed/storage/v1alpha1"
	storagev1beta1 "k8s.io/client-go/kubernetes/typed/storage/v1beta1"

	"istio.io/istio/galley/pkg/testing/common"
)

// Client is a mock implementation of kubernetes.Interface
type Client struct {
	e          *common.MockLog
	MockCoreV1 *coreV1
}

var _ kubernetes.Interface = &Client{}

// NewClient returns a new instance of Client
func NewClient() *Client {
	e := &common.MockLog{}
	return &Client{
		e: e,
		MockCoreV1: &coreV1{
			e: e,
			MockNamespaces: &namespaces{
				e: e,
			},
		},
	}
}

// String returns the log contents for this mock
func (m *Client) String() string {
	return m.e.String()
}

// CoreV1 interface method implementation
func (m *Client) CoreV1() corev1.CoreV1Interface {
	return m.MockCoreV1
}

// Discovery interface method implementation
func (m *Client) Discovery() discovery.DiscoveryInterface {
	panic("Not implemented")
}

// AdmissionregistrationV1alpha1 interface method implementation
func (m *Client) AdmissionregistrationV1alpha1() admissionregistrationv1alpha1.AdmissionregistrationV1alpha1Interface {
	panic("Not implemented")
}

// AdmissionregistrationV1beta1 interface method implementation
func (m *Client) AdmissionregistrationV1beta1() admissionregistrationv1beta1.AdmissionregistrationV1beta1Interface {
	panic("Not implemented")
}

// Admissionregistration interface method implementation
func (m *Client) Admissionregistration() admissionregistrationv1beta1.AdmissionregistrationV1beta1Interface {
	panic("Not implemented")
}

// AppsV1beta1 interface method implementation
func (m *Client) AppsV1beta1() appsv1beta1.AppsV1beta1Interface {
	panic("Not implemented")
}

// AppsV1beta2 interface method implementation
func (m *Client) AppsV1beta2() appsv1beta2.AppsV1beta2Interface {
	panic("Not implemented")
}

// AppsV1 interface method implementation
func (m *Client) AppsV1() appsv1.AppsV1Interface {
	panic("Not implemented")
}

// Apps interface method implementation
func (m *Client) Apps() appsv1.AppsV1Interface {
	panic("Not implemented")
}

// AuthenticationV1 interface method implementation
func (m *Client) AuthenticationV1() authenticationv1.AuthenticationV1Interface {
	panic("Not implemented")
}

// Authentication interface method implementation
func (m *Client) Authentication() authenticationv1.AuthenticationV1Interface {
	panic("Not implemented")
}

// AuthenticationV1beta1 interface method implementation
func (m *Client) AuthenticationV1beta1() authenticationv1beta1.AuthenticationV1beta1Interface {
	panic("Not implemented")
}

// AuthorizationV1 interface method implementation
func (m *Client) AuthorizationV1() authorizationv1.AuthorizationV1Interface {
	panic("Not implemented")
}

// Authorization interface method implementation
func (m *Client) Authorization() authorizationv1.AuthorizationV1Interface {
	panic("Not implemented")
}

// AuthorizationV1beta1 interface method implementation
func (m *Client) AuthorizationV1beta1() authorizationv1beta1.AuthorizationV1beta1Interface {
	panic("Not implemented")
}

// AutoscalingV1 interface method implementation
func (m *Client) AutoscalingV1() autoscalingv1.AutoscalingV1Interface {
	panic("Not implemented")
}

// Autoscaling interface method implementation
func (m *Client) Autoscaling() autoscalingv1.AutoscalingV1Interface {
	panic("Not implemented")
}

// AutoscalingV2beta1 interface method implementation
func (m *Client) AutoscalingV2beta1() autoscalingv2beta1.AutoscalingV2beta1Interface {
	panic("Not implemented")
}

// BatchV1 interface method implementation
func (m *Client) BatchV1() batchv1.BatchV1Interface {
	panic("Not implemented")
}

// Batch interface method implementation
func (m *Client) Batch() batchv1.BatchV1Interface {
	panic("Not implemented")
}

// BatchV1beta1 interface method implementation
func (m *Client) BatchV1beta1() batchv1beta1.BatchV1beta1Interface {
	panic("Not implemented")
}

// BatchV2alpha1 interface method implementation
func (m *Client) BatchV2alpha1() batchv2alpha1.BatchV2alpha1Interface {
	panic("Not implemented")
}

// CertificatesV1beta1 interface method implementation
func (m *Client) CertificatesV1beta1() certificatesv1beta1.CertificatesV1beta1Interface {
	panic("Not implemented")
}

// Certificates interface method implementation
func (m *Client) Certificates() certificatesv1beta1.CertificatesV1beta1Interface {
	panic("Not implemented")
}

// Core interface method implementation
func (m *Client) Core() corev1.CoreV1Interface {
	panic("Not implemented")
}

// EventsV1beta1 interface method implementation
func (m *Client) EventsV1beta1() eventsv1beta1.EventsV1beta1Interface {
	panic("Not implemented")
}

// Events interface method implementation
func (m *Client) Events() eventsv1beta1.EventsV1beta1Interface {
	panic("Not implemented")
}

// ExtensionsV1beta1 interface method implementation
func (m *Client) ExtensionsV1beta1() extensionsv1beta1.ExtensionsV1beta1Interface {
	panic("Not implemented")
}

// Extensions interface method implementation
func (m *Client) Extensions() extensionsv1beta1.ExtensionsV1beta1Interface {
	panic("Not implemented")
}

// NetworkingV1 interface method implementation
func (m *Client) NetworkingV1() networkingv1.NetworkingV1Interface {
	panic("Not implemented")
}

// Networking interface method implementation
func (m *Client) Networking() networkingv1.NetworkingV1Interface {
	panic("Not implemented")
}

// PolicyV1beta1 interface method implementation
func (m *Client) PolicyV1beta1() policyv1beta1.PolicyV1beta1Interface {
	panic("Not implemented")
}

// Policy interface method implementation
func (m *Client) Policy() policyv1beta1.PolicyV1beta1Interface {
	panic("Not implemented")
}

// RbacV1 interface method implementation
func (m *Client) RbacV1() rbacv1.RbacV1Interface {
	panic("Not implemented")
}

// Rbac interface method implementation
func (m *Client) Rbac() rbacv1.RbacV1Interface {
	panic("Not implemented")
}

// RbacV1beta1 interface method implementation
func (m *Client) RbacV1beta1() rbacv1beta1.RbacV1beta1Interface {
	panic("Not implemented")
}

// RbacV1alpha1 interface method implementation
func (m *Client) RbacV1alpha1() rbacv1alpha1.RbacV1alpha1Interface {
	panic("Not implemented")
}

// SchedulingV1alpha1 interface method implementation
func (m *Client) SchedulingV1alpha1() schedulingv1alpha1.SchedulingV1alpha1Interface {
	panic("Not implemented")
}

// Scheduling interface method implementation
func (m *Client) Scheduling() schedulingv1alpha1.SchedulingV1alpha1Interface {
	panic("Not implemented")
}

// SettingsV1alpha1 interface method implementation
func (m *Client) SettingsV1alpha1() settingsv1alpha1.SettingsV1alpha1Interface {
	panic("Not implemented")
}

// Settings interface method implementation
func (m *Client) Settings() settingsv1alpha1.SettingsV1alpha1Interface {
	panic("Not implemented")
}

// StorageV1beta1 interface method implementation
func (m *Client) StorageV1beta1() storagev1beta1.StorageV1beta1Interface {
	panic("Not implemented")
}

// StorageV1 interface method implementation
func (m *Client) StorageV1() storagev1.StorageV1Interface {
	panic("Not implemented")
}

// Storage interface method implementation
func (m *Client) Storage() storagev1.StorageV1Interface {
	panic("Not implemented")
}

// StorageV1alpha1 interface method implementation
func (m *Client) StorageV1alpha1() storagev1alpha1.StorageV1alpha1Interface {
	panic("Not implemented")
}
