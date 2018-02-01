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

package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"istio.io/istio/pkg/log"
	nagent "istio.io/istio/security/cmd/node_agent_k8s"
	pb "istio.io/istio/security/proto"
)

// Resp is the driver response
type Resp struct {
	// Status of the response
	Status string `json:"status"`
	// Response message of the response
	Message string `json:"message"`
	// Capability resp.
	Attach bool `json:"attach,omitempty"`
	// Is attached resp.
	Attached bool `json:"attached,omitempty"`
	// Dev mount resp.
	Device string `json:"device,omitempty"`
	// Volumen name resp.
	VolumeName string `json:"volumename,omitempty"`
}

// NodeAgentInputs defines the input from FlexVolume driver.
type NodeAgentInputs struct {
	UID            string `json:"kubernetes.io/pod.uid"`
	Name           string `json:"kubernetes.io/pod.name"`
	Namespace      string `json:"kubernetes.io/pod.namespace"`
	ServiceAccount string `json:"kubernetes.io/serviceAccount.name"`
}

const (
	nodeAgentMgmtAPI string = "/tmp/udsuspver/mgmt.sock"
	nodeAgentUdsHome string = "/tmp/nodeagent"
	volumeName       string = "tmpfs"
)

// Init initialize the driver
func Init(version string) error {
	if version == "1.8" {
		_, err := json.Marshal(&Resp{Status: "Success", Message: "Init ok.", Attach: false})
		if err != nil {
			return err
		}
		return nil
	}
	log.Info("Init finishes successfully")
	return nil
}

// Attach attach the driver
func Attach(opts, nodeName string) error {
	_, err := json.Marshal(&Resp{Device: volumeName, Status: "Success", Message: "Dir created"})
	if err != nil {
		log.Errorf("Failed to attach with error: %v", err)
		return err
	}
	inp := opts + "|" + nodeName
	log.Infof("Attach to %s successfully", inp)
	return nil
}

// Detach detach the driver
func Detach(devID string) error {
	_, err := json.Marshal(&Resp{Status: "Success", Message: "Gone " + devID})
	if err != nil {
		log.Errorf("Failed to detach with error: %v", err)
		return err
	}
	log.Infof("Detach to %s successfully", devID)
	return nil
}

// WaitAttach wait the driver to be attached.
func WaitAttach(dev, opts string) error {
	_, err := json.Marshal(&Resp{Device: dev, Status: "Success", Message: "Wait ok"})
	if err != nil {
		return err
	}
	inp := dev + "|" + opts
	log.Infof("Watiattach to %s successfully", inp)
	return nil
}

// IsAttached checks whether the driver is attached.
func IsAttached(opts, node string) error {
	_, err := json.Marshal(&Resp{Attached: true, Status: "Success", Message: "Is attached"})
	if err != nil {
		return err
	}
	inp := opts + "|" + node
	log.Infof("IsAttached to %s successfully", inp)
	return nil
}

// MountDev mounts the device
func MountDev(dir, dev, opts string) error {
	inp := dir + "|" + dev + "|" + opts
	log.Infof("Mountdev to %s successfully", inp)
	return nil
}

// UnmountDev unmounts the device
func UnmountDev(dev string) error {
	log.Infof("Unmountdev to %s successfully", dev)
	return nil
}

// checkValidMountOpts checks if there are sufficient inputs to
// call Nodeagent.
func checkValidMountOpts(opts string) (*pb.WorkloadInfo, bool) {
	ninputs := NodeAgentInputs{}
	err := json.Unmarshal([]byte(opts), &ninputs)
	if err != nil {
		return nil, false
	}

	attrs := pb.WorkloadInfo_WorkloadAttributes{
		Uid:            ninputs.UID,
		Workload:       ninputs.Name,
		Namespace:      ninputs.Namespace,
		Serviceaccount: ninputs.ServiceAccount}

	wlInfo := pb.WorkloadInfo{Attrs: &attrs}
	return &wlInfo, true
}

// doMount perform the actual mount work
func doMount(dstDir string, ninputs *pb.WorkloadInfo_WorkloadAttributes) error {
	newDir := nodeAgentUdsHome + "/" + ninputs.Uid
	err := os.MkdirAll(newDir, 0777)
	if err != nil {
		return err
	}

	// Not really needed but attempt to workaround:
	// https://github.com/kubernetes/kubernetes/blob/61ac9d46382884a8bd9e228da22bca5817f6d226/pkg/util/mount/mount_linux.go
	cmdMount := exec.Command("/bin/mount", "-t", "tmpfs", "-o", "size=8K", "tmpfs", dstDir)
	err = cmdMount.Run()
	if err != nil {
		os.RemoveAll(newDir)
		return err
	}

	newDstDir := dstDir + "/nodeagent"
	err = os.MkdirAll(newDstDir, 0777)
	if err != nil {
		cmd := exec.Command("/bin/unmount", dstDir)
		cmd.Run()
		os.RemoveAll(newDir)
		return err
	}

	// Do a bind mount
	cmd := exec.Command("/bin/mount", "--bind", newDir, newDstDir)
	err = cmd.Run()
	if err != nil {
		cmd = exec.Command("/bin/umount", dstDir)
		cmd.Run()
		os.RemoveAll(newDir)
		return err
	}

	return nil
}

// doUnmount perform the actual unmount work
func doUnmount(dir string) error {
	cmd := exec.Command("/bin/umount", dir)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

// addListener add the listener for the workload
func addListener(ninputs *pb.WorkloadInfo) error {
	client := nagent.ClientUds(nodeAgentMgmtAPI)
	if client == nil {
		return errors.New("failed to create Nodeagent client")
	}

	_, err := client.WorkloadAdded(ninputs)
	if err != nil {
		return err
	}

	client.Close()
	return nil
}

// delListener delete the listener for the workload
func delListener(ninputs *pb.WorkloadInfo) error {
	client := nagent.ClientUds(nodeAgentMgmtAPI)
	if client == nil {
		return errors.New("failed to create Nodeagent client")
	}

	_, err := client.WorkloadDeleted(ninputs)
	if err != nil {
		return err
	}

	client.Close()
	return nil
}

// Mount mount the file path.
func Mount(dir, opts string) error {
	inp := dir + "|" + opts

	ninputs, s := checkValidMountOpts(opts)
	if s == false {
		sErr := fmt.Sprintf("Mount failed with dir %s with incomplete inputs", inp)
		return errors.New(sErr)
	}

	if err := doMount(dir, ninputs.Attrs); err != nil {
		sErr := fmt.Sprintf("Mount failed with dir %s with error: %v", inp, err)
		return errors.New(sErr)
	}

	if err := addListener(ninputs); err != nil {
		sErr := fmt.Sprintf("Failure to notify nodeagent with error: %v", err)
		return errors.New(sErr)
	}

	log.Infof("Mount successfully with dir %s", inp)
	return nil
}

// Unmount unmount the file path.
func Unmount(dir string) error {
	comps := strings.Split(dir, "/")
	if len(comps) < 6 {
		sErr := fmt.Sprintf("Unmount failed with dir %s.", dir)
		return errors.New(sErr)
	}

	uid := comps[5]
	attrs := pb.WorkloadInfo_WorkloadAttributes{Uid: uid}

	naInp := &pb.WorkloadInfo{Attrs: &attrs}
	if err := delListener(naInp); err != nil {
		sErr := fmt.Sprintf("Failed to notify node agent with error: %v", err)
		return errors.New(sErr)
	}

	// unmount the bind mount
	doUnmount(dir + "/nodeagent")
	// unmount the tmpfs
	doUnmount(dir)
	// delete the directory that was created.
	delDir := nodeAgentUdsHome + "/" + uid
	err := os.Remove(delDir)
	if err != nil {
		sErr := fmt.Sprintf("Unmount failed when delete dir %s with error: %v", delDir, err)
		return errors.New(sErr)
	}

	log.Infof("Unmount successfully with dir %s", dir)
	return nil
}

// GetVolName get the volume name
func GetVolName(opts string) error {
	log.Infof("The opts is %s", opts)
	_, err := json.Marshal(&Resp{VolumeName: volumeName, Status: "Success", Message: "ok"})
	return err
}
