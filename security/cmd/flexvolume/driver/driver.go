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

// Package driver is invoked by kubelet when a pod installs a flexvolume drive
// of type nodeagent/uds
// This driver communicates to the nodeagent using either
//   * (Default) writing credentials of workloads to a file or
//   * gRPC message defined at protos/nodeagementmgmt.proto,
// to shares the properties of the pod with nodeagent.
//
package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log/syslog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	fv "istio.io/istio/security/pkg/flexvolume"
)

// Response is the output of Flex volume driver to the kubelet.
type Response struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	// Is attached resp.
	Attached bool `json:"attached,omitempty"`
	// Dev mount resp.
	Device string `json:"device,omitempty"`
	// Volumen name resp.
	VolumeName string `json:"volumename,omitempty"`
}

// Capabilities define whether driver is attachable and the linux relabel
type Capabilities struct {
	Attach         bool `json:"attach"`
	SELinuxRelabel bool `json:"selinuxRelabel"`
}

// InitResponse is the response to the 'init' command.
// We want to explicitly set and send Attach: false
// that is why it is separated from the Response struct.
type InitResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	// Capability resp.
	Capabilities *Capabilities `json:",omitempty"`
}

// ConfigurationOptions may be used to setup the driver.
// These are optional and most users will not depened on them and will instead use the defaults.
type ConfigurationOptions struct {
	// Version of the Kubernetes cluster on which the driver is running.
	K8sVersion string `json:"k8s_version,omitempty"`
	// Location on the node's filesystem where the driver will host the
	// per workload directory and the credentials for the workload.
	// Default: /var/run/nodeagent
	NodeAgentManagementHomeDir string `json:"nodeagent_management_home,omitempty"`
	// Relative location to NodeAgentManagementHomeDir where per workload directory
	// will be created.
	// Default: /mount
	// For example: /mount here implies /var/run/nodeagent/mount/ directory
	// on the node.
	NodeAgentWorkloadHomeDir string `json:"nodeagent_workload_home,omitempty"`
	// Relative location to NodeAgentManagementHomeDir where per workload credential
	// files will be created.
	// Default: /creds
	// For example: /creds here implies /var/run/nodeagent/creds/ directory
	NodeAgentCredentialsHomeDir string `json:"nodeagent_credentials_home,omitempty"`
	// Log level for loggint to node syslog. Options: INFO|WARNING
	// Default: WARNING
	LogLevel string `json:"log_level,omitempty"`
}

// FlexVolumeInputs is the structure used by kubelet to notify driver
// volume mounts/unmounts.
type FlexVolumeInputs struct {
	UID            string `json:"kubernetes.io/pod.uid"`
	Name           string `json:"kubernetes.io/pod.name"`
	Namespace      string `json:"kubernetes.io/pod.namespace"`
	ServiceAccount string `json:"kubernetes.io/serviceAccount.name"`
}

const (
	versionK8s        string = "1.8"
	configFileName    string = "/etc/flexvolume/nodeagent.json"
	nodeAgentHome     string = "/tmp/nodeagent"
	mountDir          string = "/mount"
	credentialDirHome string = "/creds"
	logLevelWarn      string = "WARNING"
)

var (
	// configuration is the active configuration that is being used by the driver.
	configuration *ConfigurationOptions
	// logWriter is used to notify syslog of the functionality of the driver.
	logWriter *syslog.Writer
	// defaultConfiguration is the default configuration for the driver.
	defaultConfiguration = ConfigurationOptions{
		K8sVersion:                  versionK8s,
		NodeAgentManagementHomeDir:  nodeAgentHome,
		NodeAgentWorkloadHomeDir:    mountDir,
		NodeAgentCredentialsHomeDir: credentialDirHome,
		LogLevel:                    logLevelWarn,
	}
	configFile = configFileName
	// setup as var's so that we can test.
	getExecCmd = exec.Command
)

// InitCommand handles the init command for the driver.
func InitCommand() error {
	if configuration.K8sVersion == "1.8" {
		resp, err := json.Marshal(&InitResponse{Status: "Success", Message: "Init ok.", Capabilities: &Capabilities{Attach: false}})
		if err != nil {
			return err
		}
		fmt.Println(string(resp))
		return nil
	}
	return genericSuccess("init", "", "Init ok.")
}

// produceWorkloadCredential converts input from the kubelet to WorkloadInfo
func produceWorkloadCredential(opts string) (*fv.Credential, error) {
	ninputs := FlexVolumeInputs{}
	err := json.Unmarshal([]byte(opts), &ninputs)
	if err != nil {
		return nil, err
	}

	wlInfo := fv.Credential{
		UID:            ninputs.UID,
		Workload:       ninputs.Name,
		Namespace:      ninputs.Namespace,
		ServiceAccount: ninputs.ServiceAccount,
	}
	return &wlInfo, nil
}

// doMount handles a new workload mounting the flex volume drive. It will:
// * mount a tmpfs at the destination directory of the workload created by the kubelet.
// * create a sub-directory ('nodeagent') there
// * do a bind mount of the nodeagent's directory on the node to the destinationDir/nodeagent.
func doMount(destinationDir string, cred *fv.Credential) error {
	newDir := filepath.Join(configuration.NodeAgentWorkloadHomeDir, cred.UID)
	err := os.MkdirAll(newDir, 0777)
	if err != nil {
		return err
	}

	// Not really needed but attempt to workaround:
	// https://github.com/kubernetes/kubernetes/blob/61ac9d46382884a8bd9e228da22bca5817f6d226/pkg/util/mount/mount_linux.go
	cmdMount := getExecCmd("/bin/mount", "-t", "tmpfs", "-o", "size=8K", "tmpfs", destinationDir)
	err = cmdMount.Run()
	if err != nil {
		os.RemoveAll(newDir) //nolint: errcheck
		return err
	}

	newDestinationDir := filepath.Join(destinationDir, "nodeagent")
	err = os.MkdirAll(newDestinationDir, 0777)
	if err != nil {
		getExecCmd("/bin/unmount", destinationDir).Run() //nolint: errcheck
		os.RemoveAll(newDir)                             //nolint: errcheck
		return err
	}

	// Do a bind mount
	cmd := getExecCmd("/bin/mount", "--bind", newDir, newDestinationDir)
	err = cmd.Run()
	if err != nil {
		getExecCmd("/bin/umount", destinationDir).Run() //nolint: errcheck
		os.RemoveAll(newDir)                            //nolint: errcheck
		return err
	}

	return nil
}

// Rollback the operations done by doMount
// rollback code is best effort attempts to clean up.
// nolint: errcheck
func handleErrMount(destinationDir string, cred *fv.Credential) {
	newDestinationDir := filepath.Join(destinationDir, "nodeagent")
	getExecCmd("/bin/umount", newDestinationDir).Run()
	getExecCmd("/bin/umount", destinationDir).Run()
	// Sufficient to just remove the underlying directory.
	os.RemoveAll(destinationDir)
	os.RemoveAll(filepath.Join(configuration.NodeAgentWorkloadHomeDir, cred.UID))
}

// Mount handles the mount command to the driver.
func Mount(dir, opts string) error {
	inp := strings.Join([]string{dir, opts}, "|")

	ninputs, err := produceWorkloadCredential(opts)
	if err != nil {
		return failure("mount", inp, err.Error())
	}

	if err := doMount(dir, ninputs); err != nil {
		sErr := "Failure to mount: " + err.Error()
		return failure("mount", inp, sErr)
	}

	if err := addCredentialFile(ninputs); err != nil {
		handleErrMount(dir, ninputs)
		sErr := "Failure to create credentials: " + err.Error()
		return failure("mount", inp, sErr)
	}

	return genericSuccess("mount", inp, "Mount ok.")
}

// Unmount handles the unmount command to the driver.
func Unmount(dir string) error {
	var emsgs []string
	// Stop the listener.
	comps := strings.Split(dir, "/")
	if len(comps) < 6 {
		sErr := fmt.Sprintf("Failure to notify nodeagent dir %v", dir)
		return failure("unmount", dir, sErr)
	}

	uid := comps[5]
	// TBD: Check if uid is the correct format.
	naInp := &fv.Credential{
		UID: uid,
	}
	if err := removeCredentialFile(naInp); err != nil {
		// Go ahead and finish the unmount; no need to hold up kubelet.
		emsgs = append(emsgs, "Failure to delete credentials file: "+err.Error())
	}

	// unmount the bind mount
	if err := getExecCmd("/bin/umount", filepath.Join(dir, "nodeagent")).Run(); err != nil {
		emsgs = append(emsgs, fmt.Sprintf("unmount of %s failed", filepath.Join(dir, "nodeagent")))
	}

	// unmount the tmpfs
	if err := getExecCmd("/bin/umount", dir).Run(); err != nil {
		emsgs = append(emsgs, fmt.Sprintf("unmount of %s failed", dir))
	}

	// delete the directory that was created.
	delDir := filepath.Join(configuration.NodeAgentWorkloadHomeDir, uid)
	err := os.Remove(delDir)
	if err != nil {
		emsgs = append(emsgs, fmt.Sprintf("unmount del failure %s: %s", delDir, err.Error()))
		// go head and return ok.
	}

	if len(emsgs) == 0 {
		emsgs = append(emsgs, "Unmount Ok")
	}

	return genericSuccess("unmount", dir, strings.Join(emsgs, ","))
}

// printAndLog is used to print to stdout and to the syslog.
func printAndLog(caller, inp, s string) {
	fmt.Println(s)
	logToSys(caller, inp, s)
}

// genericSuccess is to print a success response to the kubelet.
func genericSuccess(caller, inp, msg string) error {
	resp, err := json.Marshal(&Response{Status: "Success", Message: msg})
	if err != nil {
		return err
	}

	printAndLog(caller, inp, string(resp))
	return nil
}

// failure is to print a failure response to the kubelet.
func failure(caller, inp, msg string) error {
	resp, err := json.Marshal(&Response{Status: "Failure", Message: msg})
	if err != nil {
		return errors.New(msg)
	}

	printAndLog(caller, inp, string(resp))
	return errors.New(msg)
}

// GenericUnsupported is to print a un-supported response to the kubelet.
func GenericUnsupported(caller, inp, msg string) error {
	resp, err := json.Marshal(&Response{Status: "Not supported", Message: msg})
	if err != nil {
		return err
	}

	printAndLog(caller, inp, string(resp))
	return nil
}

// logToSys is to write to syslog.
func logToSys(caller, inp, opts string) {
	if logWriter == nil {
		return
	}

	opt := strings.Join([]string{caller, inp, opts}, "|")
	if configuration.LogLevel == logLevelWarn {
		logWriter.Warning(opt) //nolint: errcheck
	} else {
		logWriter.Info(opt) //nolint: errcheck
	}
}

func getCredFile(uid string) string {
	return uid + fv.CredentialFileExtension
}

// addCredentialFile is used to create a credential file when a workload with the flex-volume volume mounted is created.
func addCredentialFile(creds *fv.Credential) error {
	//Make the directory and then write the ninputs as json to it.
	err := os.MkdirAll(configuration.NodeAgentCredentialsHomeDir, 0755)
	if err != nil {
		return err
	}

	var attrs []byte
	attrs, err = json.Marshal(creds)
	if err != nil {
		return err
	}

	credsFileTmp := filepath.Join(configuration.NodeAgentManagementHomeDir, getCredFile(creds.UID))
	err = ioutil.WriteFile(credsFileTmp, attrs, 0644)
	if err != nil {
		return err
	}

	// Move it to the right location now.
	credsFile := filepath.Join(configuration.NodeAgentCredentialsHomeDir, getCredFile(creds.UID))
	return os.Rename(credsFileTmp, credsFile)
}

// removeCredentialFile is used to delete a credential file when a workload with the flex-volume volume mounted is deleted.
func removeCredentialFile(creds *fv.Credential) error {
	credsFile := filepath.Join(configuration.NodeAgentCredentialsHomeDir, getCredFile(creds.UID))
	err := os.Remove(credsFile)
	return err
}

//mkAbsolutePaths converts all the configuration paths to be absolute.
func mkAbsolutePaths(config *ConfigurationOptions) {
	config.NodeAgentWorkloadHomeDir = filepath.Join(config.NodeAgentManagementHomeDir, config.NodeAgentWorkloadHomeDir)
	config.NodeAgentCredentialsHomeDir = filepath.Join(config.NodeAgentManagementHomeDir, config.NodeAgentCredentialsHomeDir)
}

// InitConfiguration reads the configuration file (if available) and initialize the configuration options
// of the driver.
func InitConfiguration() {
	configuration = &defaultConfiguration
	mkAbsolutePaths(configuration)

	if _, err := os.Stat(configFile); err != nil {
		// Return quietly
		return
	}

	bytes, err := ioutil.ReadFile(configFile)
	if err != nil {
		logWriter.Warning(fmt.Sprintf("Not able to read %s: %s\n", configFileName, err.Error())) //nolint: errcheck
		return
	}

	var config ConfigurationOptions
	err = json.Unmarshal(bytes, &config)
	if err != nil {
		logWriter.Warning(fmt.Sprintf("Not able to parse %s: %s\n", configFileName, err.Error())) //nolint: errcheck
		return
	}

	//fill in if missing configurations
	if len(config.NodeAgentManagementHomeDir) == 0 {
		config.NodeAgentManagementHomeDir = nodeAgentHome
	}

	if len(config.NodeAgentWorkloadHomeDir) == 0 {
		config.NodeAgentWorkloadHomeDir = mountDir
	}

	if len(config.NodeAgentCredentialsHomeDir) == 0 {
		config.NodeAgentCredentialsHomeDir = credentialDirHome
	}

	if len(config.LogLevel) == 0 {
		config.LogLevel = logLevelWarn
	}

	if len(config.K8sVersion) == 0 {
		config.K8sVersion = versionK8s
	}

	configuration = &config
	mkAbsolutePaths(configuration)
}

func logLevel(level string) syslog.Priority {
	switch level {
	case logLevelWarn:
		return syslog.LOG_WARNING
	}
	return syslog.LOG_INFO
}

// InitLog is used to created the syslog with a tag for the driver.
func InitLog(tag string) (*syslog.Writer, error) {
	var err error
	logWriter, err = syslog.New(logLevel(configuration.LogLevel)|syslog.LOG_DAEMON, tag)
	return logWriter, err
}
