package util

import (
	"fmt"
  "os"
  "os/user"

	"github.com/golang/glog"
)


// Download Istioctl binary
func DownloadIstioctl(tmpDir, istioctlUrl string) (string, error) {
	usr, eUser := user.Current()
	if eUser != nil {
		return "", eUser
	}
	homeDir := usr.HomeDir

	HttpDownload(tmpDir+"/istioctl", istioctlUrl+"/istioctl-linux")
	if err := os.Chmod(fmt.Sprintf("%s/istioctl", tmpDir), 0755); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/istioctl -c %s/.kube/config", tmpDir, homeDir), nil
}

func KubeInject(yamlFile, svcName, yamlDir, istioctl, proxyHub, proxyTag, namespace string) (string, error) {
  injectedYamlFile := yamlDir + "injected-" + svcName + "-app.yaml"
  if _, err := Shell(fmt.Sprintf("%s kube-inject -f %s -o %s --hub %s --tag %s -n %s", istioctl, yamlFile, injectedYamlFile, proxyHub, proxyTag, namespace)); err != nil {
    glog.Errorf("Kube-inject failed for service %s", svcName)
    return "", err
  }
  return injectedYamlFile, nil
}
