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

var (
	// flags to select vm with specific configuration
	// TODO(incfly): delete these flags.
	masonInfoFile = flag.String("mason_info", "", "File created by Mason Client that provides information about the SUT")
	projectID     = flag.String("project_id", "istio-testing", "Project ID")
	projectNumber = flag.String("project_number", "450874614208", "Project Number")
	zone          = flag.String("zone", "us-east4-c", "The zone in which the VM and cluster resides")
	clusterName   = flag.String("cluster_name", "", "The name of the istio cluster that the VM extends")
	debURL        = flag.String("deb_url", "", "The URL where `istio-sidecar.deb` can be accessed")
	// paths
	setupMeshExScript  = ""
	setupIstioVMScript = ""
)

const (
	debURLFmt    = "https://storage.googleapis.com/istio-artifacts/%s/%s/artifacts/debs"
	image        = "debian-9-stretch-v20170816"
	imageProject = "debian-cloud"
)

// setupMeshExOpts includes the options to run the setupMeshEx.sh script.
// TODO(incfly): refactor setupMeshEx.sh to use different environment var for cluster and vm zone.
type setupMeshExOpts struct {
	// zone will be set as an evironment variable.
	zone string
	// args is passing as cmd flag to setupMeshEx script.
	args []string
}

// GCPRawVM is hosted on Google Cloud Platform.
// TODO(incfly): change all these to lower case as private fields.
type GCPRawVM struct {
	Name        string
	ClusterName string
	// ClusterZone is the zone where GKE cluster locates.
	ClusterZone   string
	Namespace     string
	ProjectID     string
	projectNumber string
	// Zone is the GCP zone where GCE instances locates.
	Zone string
	// Use Mason does not require provisioning, and therefore all following fields are not required
	UseMason  bool
	debianURL string
	sshUser   string
}

// GCPVMOpts specifies the options when creating a new GCE instance for mesh expansion.
// TODO: add a validation method, either two cases are valid, only MasonInfoPath is
type GCPVMOpts struct {
	MasonInfoPath string `json:"mason_info_path"`
	Namespace     string `json:"vm_namespace"`
	ProjectNumber string `json:"project_number"`
	ProjectID     string `json:"project_id"`
	Zone          string `json:"gcp_vm_zone"`
	ClusterName   string `json:"gke_cluster_name"`
	DebianURL     string `json:"sidecar_debian_url"`
	SSHUser       string `json:"vm_ssh_user"`
}

// NewGCPRawVM creates a new vm on GCP.
func NewGCPRawVM(opts GCPVMOpts) (*GCPRawVM, error) {
	if opts.MasonInfoPath != "" {
		info, err := parseInfoFile(opts.MasonInfoPath)
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
		Name:          vmName,
		ClusterName:   opts.MasonInfoPath,
		Namespace:     opts.Namespace,
		ProjectID:     opts.ProjectID,
		projectNumber: opts.ProjectNumber,
		Zone:          opts.Zone,
		debianURL:     opts.DebianURL,
		sshUser:       opts.SSHUser,
	}, nil
}

// We use default service account.
// N.B, to use other service account, it must have iam.ServiceAccountActor or owner permissions.
func (vm *GCPRawVM) serviceAccount() string {
	return fmt.Sprintf("%s-compute@developer.gserviceaccount.com", vm.projectNumber)
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
	if vm.sshUser != "" {
		sshUserPrefix = vm.sshUser + "@"
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
	if !vm.UseMason {
		_, err := u.Shell(vm.baseCommand("delete"))
		return err
	}
	return nil
}

// Setup initializes the VM.
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
	if err := vm.buildIstioVersion(); err != nil {
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

func (vm *GCPRawVM) buildIstioVersion() error {
	url := fmt.Sprintf(debURLFmt, "pilot", *pilotTag)
	if vm.debianURL != "" {
		url = vm.debianURL
	}
	// `install/tools/setupIstioVM.sh` sources istio.VERSION to
	// get `istio-sidecar.deb` from PILOT_DEBIAN_URL
	urls := fmt.Sprintf(`export PILOT_DEBIAN_URL="%s";`, url)
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
			vm.serviceAccount(), image, imageProject)
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
	if *sshUser != "" {
		env = fmt.Sprintf("%v\nexport GCP_SSH_USER=%v;\n", env, *sshUser)
	}
	cmd := fmt.Sprintf("%s %s %s", setupMeshExScript, op, argsStr)
	_, err := u.Shell(env + cmd)
	return err
}

func prepareConstants() error {
	root, err := u.GitRootDir()
	if err != nil {
		return err
	}
	setupMeshExScript = filepath.Join(root, "install/tools/setupMeshEx.sh")
	setupIstioVMScript = filepath.Join(root, "install/tools/setupIstioVM.sh")
	return nil
}
