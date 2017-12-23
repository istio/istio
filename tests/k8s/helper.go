package k8s

import (
	"log"
	"os"
	"os/user"
)

// Kubeconfig returns the config to use for testing.
func Kubeconfig(relpath string) string {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig != "" {
		return kubeconfig
	}

	// For Bazel sandbox we search a different location:
	// Attempt to use the relpath, using the linked file - pilot/kube/platform/config
	kubeconfig, _ = os.Getwd()
	kubeconfig = kubeconfig + relpath
	if _, err := os.Stat(kubeconfig); err == nil {
		return kubeconfig
	}

	// Fallback to the user's default config
	log.Println("Using user home k8s config - might affect real cluster ! Not found: ", kubeconfig)
	usr, err := user.Current()
	if err == nil {
		kubeconfig = usr.HomeDir + "/.kube/config"
	}

	return kubeconfig
}
