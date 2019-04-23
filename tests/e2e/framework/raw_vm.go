// Copyright 2017 Istio Authors
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

package framework

import (
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	u "istio.io/istio/tests/util"
)

// RawVM interfaces different cloud venders to support e2e testing with VM
type RawVM interface {
	Cleanable
	GetInternalIP() (string, error)
	GetExternalIP() (string, error)
	SecureShell(cmd string) (string, error)
	SecureCopy(files ...string) (string, error)
}

// TODO(incfly): rename this flags to istio.xx.yy format?
var (
	// flags to select vm with specific configuration
	masonInfoFile = flag.String("mason_info", "", "File created by Mason Client that provides information about the SUT")
	projectID     = flag.String("project_id", "istio-testing", "Project ID")
	projectNumber = flag.String("project_number", "450874614208", "Project Number")
	zone          = flag.String("zone", "us-east4-c", "The zone in which the VM and cluster resides")
	clusterName   = flag.String("cluster_name", "", "The name of the istio cluster that the VM extends")
	image         = flag.String("image", "debian-9-stretch-v20170816", "Image Name")
	imageProject  = flag.String("image_project", "debian-cloud", "Image Project")
	debURL        = flag.String("deb_url", "", "The URL where `istio-sidecar.deb` can be accessed")
	sshUser       = flag.String("istio.test.prow.gceuser", "", "The user for gcloud compute ssh into.")
	// paths
	setupMeshExScript  = ""
	mashExpansionYaml  = ""
	setupIstioVMScript = ""
	// kubectl delete resources on tear down
	kubeDeleteYaml = []string{}
)

const (
	debURLFmt = "https://storage.googleapis.com/istio-artifacts/%s/%s/artifacts/debs"
)

// setupMeshExOpts includes the options to run the setupMeshEx.sh script.
// TODO(incfly): refactor setupMeshEx.sh to use different environment var.
type setupMeshExOpts struct {
	// zone will be set as an evironment variable.
	zone string
	// args is passing as cmd flag to setupMeshEx script.
	args []string
}

// GCPRawVM is hosted on Google Cloud Platform
type GCPRawVM struct {
	Name        string
	ClusterName string
	// ClusterZone is the zone where GKE cluster locates.
	ClusterZone string
	Namespace   string
	ProjectID   string
	// Zone is the GCP zone where GCE instances locates.
	Zone string
	// Use Mason does not require provisioning, and therefore all following fields are not required
	UseMason bool
	// ServiceAccount must have iam.serviceAccountActor or owner permissions
	// to the project. Use IAM settings.
	ServiceAccount string
	Image          string
	ImageProject   string
}

type GCPVMOpts struct {
	// Namespace is the namespace for the app running on VM belongs to.
	Namespace string
}

// NewGCPRawVM creates a new vm on GCP
func NewGCPRawVM(opts GCPVMOpts) (*GCPRawVM, error) {
	if *masonInfoFile != "" {
		info, err := parseInfoFile(*masonInfoFile)
		if err != nil {
			return nil, err
		}
		g, err := resourceInfoToGCPRawVM(*info, opts.Namespace)
		if err != nil {
			return nil, err
		}
		return g, nil
	}
	vmName := fmt.Sprintf("vm-%v", time.Now().UnixNano())
	return &GCPRawVM{
		Name:           vmName,
		ClusterName:    *clusterName,
		Namespace:      opts.Namespace,
		ServiceAccount: fmt.Sprintf("%s-compute@developer.gserviceaccount.com", *projectNumber),
		ProjectID:      *projectID,
		Zone:           *zone,
		Image:          *image,
		ImageProject:   *imageProject,
	}, nil
}

// GetInternalIP returns the internal IP of the VM
func (vm *GCPRawVM) GetInternalIP() (string, error) {
	cmd := vm.baseCommand("describe") +
		" --format='value(networkInterfaces[0].accessConfigs[0].natIP)'"
	o, e := u.Shell(cmd)
	return strings.Trim(o, "\n"), e
}

// GetExternalIP returns the internal IP of the VM
func (vm *GCPRawVM) GetExternalIP() (string, error) {
	cmd := vm.baseCommand("describe") +
		" --format='value(networkInterfaces[0].networkIP)'"
	o, e := u.Shell(cmd)
	return strings.Trim(o, "\n"), e
}

// SecureShell execeutes cmd on vm through ssh
func (vm *GCPRawVM) SecureShell(cmd string) (string, error) {
	sshUserPrefix := ""
	if *sshUser != "" {
		sshUserPrefix = *sshUser + "@"
	}
	ssh := fmt.Sprintf("gcloud compute ssh -q --project %s --zone %s %s%s --command \"%s\"",
		vm.ProjectID, vm.Zone, sshUserPrefix, vm.Name, cmd)
	return u.Shell(ssh)
}

// SecureCopy copies files to vm via scp
func (vm *GCPRawVM) SecureCopy(files ...string) (string, error) {
	filesStr := strings.Join(files, " ")
	scp := fmt.Sprintf("gcloud compute scp -q --project %s --zone %s %s Prow@%s",
		vm.ProjectID, vm.Zone, filesStr, vm.Name)
	return u.Shell(scp)
}

// Teardown releases the VM to resource manager
func (vm *GCPRawVM) Teardown() error {
	for _, yaml := range kubeDeleteYaml {
		cmd := fmt.Sprintf("cat <<EOF | kubectl delete -f -\n%sEOF", yaml)
		if _, err := u.Shell(cmd); err != nil {
			return err
		}
	}
	if !vm.UseMason {
		_, err := u.Shell(vm.baseCommand("delete"))
		return err
	}
	return nil
}

// Setup initialize the VM
func (vm *GCPRawVM) Setup() error {
	if err := prepareConstants(); err != nil {
		return err
	}
	if err := vm.provision(); err != nil {
		return fmt.Errorf("gce vm provision failed %v", err)
	}
	opts := setupMeshExOpts{
		zone: vm.ClusterZone,
		args: []string{vm.ClusterName},
	}
	if err := vm.setupMeshEx("generateClusterEnv", opts); err != nil {
		return err
	}
	if _, err := u.Shell("cat cluster.env"); err != nil {
		return err
	}
	if err := vm.setupMeshEx("generateDnsmasq", setupMeshExOpts{}); err != nil {
		return err
	}
	if _, err := u.Shell("cat kubedns"); err != nil {
		return err
	}
	if err := buildIstioVersion(); err != nil {
		return err
	}
	if _, err := u.Shell("cat istio.VERSION"); err != nil {
		return err
	}
	opts = setupMeshExOpts{
		args: []string{vm.Name},
	}
	return vm.setupMeshEx("machineSetup", opts)
}

func buildIstioVersion() error {
	if *debURL == "" {
		*debURL = fmt.Sprintf(debURLFmt, "pilot", *pilotTag)
	}
	// `install/tools/setupIstioVM.sh` sources istio.VERSION to
	// get `istio-sidecar.deb` from PILOT_DEBIAN_URL
	urls := fmt.Sprintf(`export PILOT_DEBIAN_URL="%s";`, *debURL)
	return u.WriteTextFile("istio.VERSION", urls)
}

func (vm *GCPRawVM) provision() error {
	if !vm.UseMason {
		// Create the VM
		createVMcmd := vm.baseCommand("create") + fmt.Sprintf(
			`--machine-type "n1-standard-1" \
				 --subnet default \
				 --can-ip-forward \
				 --service-account %s \
				 --scopes "https://www.googleapis.com/auth/cloud-platform" \
				 --tags "http-server","https-server" \
				 --image %s \
				 --image-project %s \
				 --boot-disk-size "10" \
				 --boot-disk-type "pd-standard" \
				 --boot-disk-device-name "debtest"`,
			vm.ServiceAccount, vm.Image, vm.ImageProject)
		if _, err := u.Shell(createVMcmd); err != nil {
			return err
		}
	}
	// wait until VM is up and ready
	isVMLive := func() (bool, error) {
		_, err := vm.SecureShell("echo hello")
		return err == nil, nil
	}
	return u.Poll(20*time.Second, 10, isVMLive)
}

func (vm *GCPRawVM) baseCommand(action string) string {
	return fmt.Sprintf("gcloud compute --project %s instances %s %s --zone %s ",
		vm.ProjectID, action, vm.Name, vm.Zone)
}

func (vm *GCPRawVM) setupMeshEx(op string, opts setupMeshExOpts) error {
	argsStr := strings.Join(opts.args, " ")
	zone := vm.Zone
	if opts.zone != "" {
		zone = opts.zone
	}
	env := fmt.Sprintf(`
		export GCP_OPTS="--project %s --zone %s";
		export SETUP_ISTIO_VM_SCRIPT="%s";`,
		vm.ProjectID, zone, setupIstioVMScript)
	cmd := fmt.Sprintf("%s %s %s", setupMeshExScript, op, argsStr)
	_, err := u.Shell(env + cmd)
	return err
}

// Initialize the K8S cluster, generating config files for the raw VMs.
// Must be run once, will generate files in the CWD. The files must be installed on the VM.
// This assumes the recommended dnsmasq config option.
func (vm *GCPRawVM) prepareCluster() error {
	kv := map[string]string{
		"istio-system": vm.Namespace,
	}
	return replaceKVInYamlThenKubectlApply(mashExpansionYaml, kv)
}

func replaceKVInYamlThenKubectlApply(yamlPath string, kv map[string]string) error {
	bytes, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return err
	}
	yaml := string(bytes)
	for k, v := range kv {
		yaml = strings.Replace(yaml, k, v, -1)
	}
	cmd := fmt.Sprintf("cat <<EOF | kubectl apply -f -\n%sEOF", yaml)
	if _, err = u.Shell(cmd); err != nil {
		return err
	}
	kubeDeleteYaml = append(kubeDeleteYaml, yaml)
	return nil
}

func prepareConstants() error {
	root, err := u.GitRootDir()
	if err != nil {
		return err
	}
	setupMeshExScript = filepath.Join(root, "install/tools/setupMeshEx.sh")
	mashExpansionYaml = filepath.Join(root, "install/kubernetes/mesh-expansion.yaml")
	setupIstioVMScript = filepath.Join(root, "install/tools/setupIstioVM.sh")
	return nil
}
