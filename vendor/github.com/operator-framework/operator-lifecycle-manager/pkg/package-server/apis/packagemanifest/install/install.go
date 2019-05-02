package install

import (
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/packagemanifest/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Install registers the API group and adds types to a scheme
func Install(scheme *runtime.Scheme) {
	v1alpha1.AddToScheme(scheme)
}
