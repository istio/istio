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
	"strings"
	"os"
	"os/exec"

	"istio.io/istio/pkg/log"
	pb "istio.io/istio/security/proto"
)

type Resp struct {
	Status  string `json:"status"`
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

type NodeAgentInputs struct {
	UID            string `json:"kubernetes.io/pod.uid"`
	Name           string `json:"kubernetes.io/pod.name"`
	Namespace      string `json:"kubernetes.io/pod.namespace"`
	ServiceAccount string `json:"kubernetes.io/serviceAccount.name"`
}

const (
	ver              string = "1.8"
	volumeName       string = "tmpfs"
	NodeAgentMgmtAPI string = "/tmp/udsuspver/mgmt.sock"
	NodeAgentUdsHome string = "/tmp/nodeagent"
)

// Init initialize the driver
func Init(ver string) error {
	if ver == "1.8" {
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
		log.Errorf("Failed to attach with error: %s", err.Error())
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
		log.Errorf("Failed to detach with error: %s", err.Error())
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

func MountDev(dir, dev, opts string) error {
	inp := dir + "|" + dev + "|" + opts
	log.Infof("Mountdev to %s successfully", inp)
	return nil
}

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
		UID: ninputs.UID,
		Workload:       ninputs.Name,
		Namespace:      ninputs.Namespace,
		Serviceaccount: ninputs.ServiceAccount}

	wlInfo := pb.WorkloadInfo{Attrs: &attrs}
	return &wlInfo, true
}

// doMount perform the actual mount work
func doMount(dstDir string, ninputs *pb.WorkloadInfo_WorkloadAttributes) error {
	newDir := NodeAgentUdsHome + "/" + ninputs.UID
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

// Mount mount the file path.
func Mount(dir, opts string) error {
	inp := dir + "|" + opts

	ninputs, s := checkValidMountOpts(opts)
	if s == false {
		sErr := fmt.Sprintf("Mount failed with dir %s with incomplete inputs", inp)
		return errors.New(sErr)
	}

	if err := doMount(dir, ninputs.Attrs); err != nil {
		sErr := fmt.Sprintf("Mount failed with dir %s with error: %s", inp, err.Error())
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

	// unmount the bind mount
	doUnmount(dir + "/nodeagent")
	// unmount the tmpfs
	doUnmount(dir)
	// delete the directory that was created.
	delDir := NodeAgentUdsHome + "/" + uid
	err := os.Remove(delDir)
	if err != nil {
		sErr := fmt.Sprintf("Unmount failed when delete dir %s with error: %s", delDir, err.Error())
		return errors.New(sErr)
	}

	log.Infof("Unmount successfully with dir %s", dir)
  return nil
}
