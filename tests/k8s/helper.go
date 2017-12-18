package k8s

import (
	"os"
	"os/user"
)

// Kubeconfig returns the config to use for testing.
func Kubeconfig(relpath string) string {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return kubeconfig
	}

	usr, err := user.Current()
	if err == nil {
		kubeconfig = usr.HomeDir + "/.kube/config"
	}

	// For Bazel sandbox we search a different location:
	if _, err = os.Stat(kubeconfig); err != nil {
		kubeconfig, _ = os.Getwd()
		kubeconfig = kubeconfig + "/../../../platform/kube/config"
	}

	return kubeconfig
}
