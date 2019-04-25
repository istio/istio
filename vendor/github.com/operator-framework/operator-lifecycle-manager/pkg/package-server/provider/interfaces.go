package provider

import (
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/packagemanifest/v1alpha1"
)

type PackageManifestProvider interface {
	Get(name, namespace string) (*v1alpha1.PackageManifest, error)
	List(namespace string) (*v1alpha1.PackageManifestList, error)
}
