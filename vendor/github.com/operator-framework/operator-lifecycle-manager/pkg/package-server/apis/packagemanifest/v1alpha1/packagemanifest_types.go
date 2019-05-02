package v1alpha1

import (
	"github.com/coreos/go-semver/semver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PackageManifestList is a list of PackageManifest objects.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PackageManifestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PackageManifest `json:"items"`
}

// PackageManifest holds information about a package, which is a reference to one (or more)
// channels under a single package.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PackageManifest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PackageManifestSpec   `json:"spec,omitempty"`
	Status PackageManifestStatus `json:"status,omitempty"`
}

// PackageManifestSpec defines the desired state of PackageManifest
type PackageManifestSpec struct{}

// PackageManifestStatus represents the current status of the PackageManifest
type PackageManifestStatus struct {
	// CatalogSource is the name of the CatalogSource this package belongs to
	CatalogSource            string `json:"catalogSource"`
	CatalogSourceDisplayName string `json:"catalogSourceDisplayName"`
	CatalogSourcePublisher   string `json:"catalogSourcePublisher"`

	//  CatalogSourceNamespace is the namespace of the owning CatalogSource
	CatalogSourceNamespace string `json:"catalogSourceNamespace"`

	// Provider is the provider of the PackageManifest's default CSV
	Provider AppLink `json:"provider,omitempty"`

	// PackageName is the name of the overall package, ala `etcd`.
	PackageName string `json:"packageName"`

	// Channels are the declared channels for the package, ala `stable` or `alpha`.
	Channels []PackageChannel `json:"channels"`

	// DefaultChannel is, if specified, the name of the default channel for the package. The
	// default channel will be installed if no other channel is explicitly given. If the package
	// has a single channel, then that channel is implicitly the default.
	DefaultChannel string `json:"defaultChannel"`
}

// GetDefaultChannel gets the default channel or returns the only one if there's only one. returns empty string if it
// can't determine the default
func (m PackageManifest) GetDefaultChannel() string {
	if m.Status.DefaultChannel != "" {
		return m.Status.DefaultChannel
	}
	if len(m.Status.Channels) == 1 {
		return m.Status.Channels[0].Name
	}
	return ""
}

// PackageChannel defines a single channel under a package, pointing to a version of that
// package.
type PackageChannel struct {
	// Name is the name of the channel, e.g. `alpha` or `stable`
	Name string `json:"name"`

	// CurrentCSV defines a reference to the CSV holding the version of this package currently
	// for the channel.
	CurrentCSV string `json:"currentCSV"`

	// CurrentCSVSpec holds the spec of the current CSV
	CurrentCSVDesc CSVDescription `json:"currentCSVDesc,omitempty"`
}

// CSVDescription defines a description of a CSV
type CSVDescription struct {
	// DisplayName is the CSV's display name
	DisplayName string `json:"displayName,omitempty"`

	// Icon is the CSV's base64 encoded icon
	Icon []Icon `json:"icon,omitempty"`

	// Version is the CSV's semantic version
	// +k8s:openapi-gen=false
	Version semver.Version `json:"version,omitempty"`

	// Provider is the CSV's provider
	Provider    AppLink           `json:"provider,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	// LongDescription is the CSV's description
	LongDescription string `json:"description,omitempty"`
}

// AppLink defines a link to an application
type AppLink struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Icon defines a base64 encoded icon and media type
type Icon struct {
	Base64Data string `json:"base64data,omitempty"`
	Mediatype  string `json:"mediatype,omitempty"`
}

// IsDefaultChannel returns true if the PackageChannel is the default for the PackageManifest
func (pc PackageChannel) IsDefaultChannel(pm PackageManifest) bool {
	return pc.Name == pm.Status.DefaultChannel || len(pm.Status.Channels) == 1
}
