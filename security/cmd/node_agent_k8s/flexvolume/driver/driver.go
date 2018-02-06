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
	"log/syslog"
	"os"
	"os/exec"
	"strings"

	nagent "istio.io/istio/security/cmd/node_agent_k8s/nodeagentmgmt"
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
	// Defines the driver's capability
	Capabilities *Capabilities `json:",omitempty"`
}

// Capabilities define whether driver is attachable and the linux relabel
type Capabilities struct {
	Attach         bool `json:"attach"`
	SELinuxRelabel bool `json:"selinuxRelabel"`
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

var (
	logWrt *syslog.Writer
)

// Init initialize the driver
func Init(version string) error {
	if version == "1.8" {
		resp, err := json.Marshal(&Resp{Status: "Success", Message: "Init ok.", Capabilities: &Capabilities{Attach: false}})
		if err != nil {
			return err
		}
		fmt.Println(string(resp))
		return nil
	}
	return genericSucc("init", "", "Init ok.")
}

// Attach attach the driver
func Attach(opts, nodeName string) error {
	resp, err := json.Marshal(&Resp{Device: volumeName, Status: "Success", Message: "Dir created"})
	if err != nil {
		return err
	}
	fmt.Println(string(resp))
	inp := opts + "|" + nodeName
	logToSys("attach", inp, string(resp))
	return nil
}

// Detach detach the driver
func Detach(devID string) error {
	resp, err := json.Marshal(&Resp{Status: "Success", Message: "Gone " + devID})
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Println(string(resp))
	logToSys("detach", devID, string(resp))
	return nil
}

// WaitAttach wait the driver to be attached.
func WaitAttach(dev, opts string) error {
	resp, err := json.Marshal(&Resp{Device: dev, Status: "Success", Message: "Wait ok"})
	if err != nil {
		return err
	}
	fmt.Println(string(resp))
	inp := dev + "|" + opts
	logToSys("waitattach", inp, string(resp))
	return nil
}

// IsAttached checks whether the driver is attached.
func IsAttached(opts, node string) error {
	resp, err := json.Marshal(&Resp{Attached: true, Status: "Success", Message: "Is attached"})
	if err != nil {
		return err
	}
	sResp := string(resp)
	fmt.Println(sResp)
	inp := opts + "|" + node
	logToSys("isattached", inp, sResp)
	return nil
}

// MountDev mounts the device
func MountDev(dir, dev, opts string) error {
	inp := dir + "|" + dev + "|" + opts
	return genericSucc("mountdev", inp, "Mount dev ok.")
}

// UnmountDev unmounts the device
func UnmountDev(dev string) error {
	return genericSucc("unmountdev", dev, "Unmount dev ok.")
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
		return Failure("mount", inp, "Incomplete inputs")
	}

	if err := doMount(dir, ninputs.Attrs); err != nil {
		sErr := "Failure to mount: " + err.Error()
		return Failure("mount", inp, sErr)
	}

	if err := addListener(ninputs); err != nil {
		sErr := "Failure to notify nodeagent: " + err.Error()
		return Failure("mount", inp, sErr)
	}

	return genericSucc("mount", inp, "Mount ok.")
}

// Unmount unmount the file path.
func Unmount(dir string) error {
	comps := strings.Split(dir, "/")
	if len(comps) < 6 {
		sErr := fmt.Sprintf("Unmount failed with dir %s.", dir)
		return Failure("unmount", dir, sErr)
	}

	uid := comps[5]
	// TBD: Check if uid is the correct format.
	attrs := pb.WorkloadInfo_WorkloadAttributes{Uid: uid}
	naInp := &pb.WorkloadInfo{Attrs: &attrs}
	if err := delListener(naInp); err != nil {
		sErr := "Failure to notify nodeagent: " + err.Error()
		return Failure("unmount", dir, sErr)
	}

	// unmount the bind mount
	doUnmount(dir + "/nodeagent")
	// unmount the tmpfs
	doUnmount(dir)
	// delete the directory that was created.
	delDir := nodeAgentUdsHome + "/" + uid
	err := os.Remove(delDir)
	if err != nil {
		estr := fmt.Sprintf("unmount del failure %s: %s", delDir, err.Error())
		return genericSucc("unmount", dir, estr)
	}

	return genericSucc("unmount", dir, "Unmount ok.")
}

// GetVolName get the volume name
func GetVolName(opts string) error {
	return genericUnsupported("getvolname", opts, "not supported")
}

func printAndLog(caller, inp, s string) {
	fmt.Println(s)
	logToSys(caller, inp, s)
}

func genericSucc(caller, inp, msg string) error {
	resp, err := json.Marshal(&Resp{Status: "Success", Message: msg})
	if err != nil {
		return err
	}

	printAndLog(caller, inp, string(resp))
	return nil
}

// Failure report failure case
func Failure(caller, inp, msg string) error {
	resp, err := json.Marshal(&Resp{Status: "Failure", Message: msg})
	if err != nil {
		return err
	}

	printAndLog(caller, inp, string(resp))
	return nil
}

func genericUnsupported(caller, inp, msg string) error {
	resp, err := json.Marshal(&Resp{Status: "Not supported", Message: msg})
	if err != nil {
		return err
	}

	printAndLog(caller, inp, string(resp))
	return nil
}

func logToSys(caller, inp, opts string) {
	if logWrt == nil {
		return
	}

	op := caller + "|"
	op = op + inp + "|"
	op = op + opts
	logWrt.Warning(op)
}
