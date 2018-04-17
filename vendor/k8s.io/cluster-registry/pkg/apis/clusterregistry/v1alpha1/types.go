/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Cluster contains information about a cluster in a cluster registry.
type Cluster struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec is the specification of the cluster. This may or may not be
	// reconciled by an active controller.
	// +optional
	Spec ClusterSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status is the status of the cluster. It is optional, and can be left nil
	// to imply that the cluster status is not being reported.
	// +optional
	Status *ClusterStatus `json:"status" protobuf:"bytes,3,opt,name=status"`
}

// ClusterSpec contains the specification of a cluster.
type ClusterSpec struct {
	// KubernetesAPIEndpoints represents the endpoints of the API server for this
	// cluster.
	// +optional
	KubernetesAPIEndpoints KubernetesAPIEndpoints `json:"kubernetesApiEndpoints,omitempty" protobuf:"bytes,1,opt,name=kubernetesApiEndpoints"`

	// AuthInfo contains public information that can be used to authenticate
	// to and authorize with this cluster. It is not meant to store private
	// information (e.g., tokens or client certificates) and cluster registry
	// implementations are not expected to provide hardened storage for
	// secrets.
	// +optional
	AuthInfo AuthInfo `json:"authInfo,omitempty" protobuf:"bytes,2,opt,name=authInfo"`

	// CloudProvider contains information about the cloud provider this cluster
	// is running on.
	// +optional
	CloudProvider *CloudProvider `json:"cloudProvider" protobuf:"bytes,3,opt,name=cloudProvider"`
}

// ClusterStatus contains the status of a cluster.
type ClusterStatus struct {
	// TODO https://github.com/kubernetes/cluster-registry/issues/28
}

// KubernetesAPIEndpoints represents the endpoints for one and only one
// Kubernetes API server.
type KubernetesAPIEndpoints struct {
	// ServerEndpoints specifies the address(es) of the Kubernetes API server’s
	// network identity or identities.
	// +optional
	ServerEndpoints []ServerAddressByClientCIDR `json:"serverEndpoints,omitempty" protobuf:"bytes,1,rep,name=serverEndpoints"`

	// CABundle contains the certificate authority information.
	// +optional
	CABundle []byte `json:"caBundle,omitempty" protobuf:"bytes,2,opt,name=caBundle"`
}

// ServerAddressByClientCIDR helps clients determine the server address that
// they should use, depending on the ClientCIDR that they match.
type ServerAddressByClientCIDR struct {
	// The CIDR with which clients can match their IP to figure out if they should
	// use the corresponding server address.
	// +optional
	ClientCIDR string `json:"clientCIDR,omitempty" protobuf:"bytes,1,opt,name=clientCIDR"`
	// Address of this server, suitable for a client that matches the above CIDR.
	// This can be a hostname, hostname:port, IP or IP:port.
	// +optional
	ServerAddress string `json:"serverAddress,omitempty" protobuf:"bytes,2,opt,name=serverAddress"`
}

// AuthInfo holds public information that describes how a client can get
// credentials to access the cluster. For example, OAuth2 client registration
// endpoints and supported flows, or Kerberos servers locations.
//
// It should not hold any private or sensitive information.
type AuthInfo struct {
	// AuthProviders is a list of configurations for auth providers.
	// +optional
	Providers []AuthProviderConfig `json:"providers" protobuf:"bytes,1,rep,name=providers"`
}

// AuthProviderConfig contains the information necessary for a client to
// authenticate to a Kubernetes API server. It is modeled after
// k8s.io/client-go/tools/clientcmd/api/v1.AuthProviderConfig.
type AuthProviderConfig struct {
	// Name is the name of this configuration.
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Type contains type information about this auth provider. Clients of the
	// cluster registry should use this field to differentiate between different
	// kinds of authentication providers.
	// +optional
	Type AuthProviderType `json:"type,omitempty" protobuf:"bytes,2,opt,name=type"`

	// Config is a map of values that contains the information necessary for a
	// client to determine how to authenticate to a Kubernetes API server.
	// +optional
	Config map[string]string `json:"config,omitempty" protobuf:"bytes,3,rep,name=config"`
}

// AuthProviderType contains metadata about the auth provider. It should be used
// by clients to differentiate between different kinds of auth providers, and to
// select a relevant provider for the client's configuration. For example, a
// controller would look for a provider type that denotes a service account
// that it should use to access the cluster, whereas a user would look for a
// provider type that denotes an authentication system from which they should
// request a token.
type AuthProviderType struct {
	// Name is the name of the auth provider.
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
}

// CloudProvider contains information about the cloud provider this cluster is
// running on.
type CloudProvider struct {
	// Name is the name of the cloud provider for this cluster.
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterList is a list of Kubernetes clusters in the cluster registry.
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// List of Cluster objects.
	Items []Cluster `json:"items" protobuf:"bytes,2,rep,name=items"`
}
