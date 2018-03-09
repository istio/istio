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
		_ = os.RemoveAll(newDir)
		return err
	}

	newDstDir := dstDir + "/nodeagent"
	err = os.MkdirAll(newDstDir, 0777)
	if err != nil {
		cmd := exec.Command("/bin/unmount", dstDir)
		_ = cmd.Run()
		_ = os.RemoveAll(newDir)
		return err
	}

	// Do a bind mount
	cmd := exec.Command("/bin/mount", "--bind", newDir, newDstDir)
	err = cmd.Run()
	if err != nil {
		cmd = exec.Command("/bin/umount", dstDir)
		_ = cmd.Run()
		_ = os.RemoveAll(newDir)
		return err
	}

	return nil
}

// doUnmount perform the actual unmount work
func doUnmount(dir string) error {
	cmd := exec.Command("/bin/umount", dir)
	return cmd.Run()
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
	if !s {
		return failure("mount", inp, "Incomplete inputs")
	}

	if err := doMount(dir, ninputs.Attrs); err != nil {
		sErr := "Failure to mount: " + err.Error()
		return failure("mount", inp, sErr)
	}

	if err := addListener(ninputs); err != nil {
		sErr := "Failure to notify nodeagent: " + err.Error()
		return failure("mount", inp, sErr)
	}

	return genericSucc("mount", inp, "Mount ok.")
}

// Unmount unmount the file path.
func Unmount(dir string) error {
	comps := strings.Split(dir, "/")
	if len(comps) < 6 {
		sErr := fmt.Sprintf("Unmount failed with dir %s.", dir)
		return failure("unmount", dir, sErr)
	}

	uid := comps[5]
	// TBD: Check if uid is the correct format.
	attrs := pb.WorkloadInfo_WorkloadAttributes{Uid: uid}
	naInp := &pb.WorkloadInfo{Attrs: &attrs}
	if err := delListener(naInp); err != nil {
		sErr := "Failure to notify nodeagent: " + err.Error()
		return failure("unmount", dir, sErr)
	}

	// unmount the bind mount
	_ = doUnmount(dir + "/nodeagent")
	// unmount the tmpfs
	_ = doUnmount(dir)
	// delete the directory that was created.
	delDir := nodeAgentUdsHome + "/" + uid
	err := os.Remove(delDir)
	if err != nil {
		estr := fmt.Sprintf("unmount del failure %s: %s", delDir, err.Error())
		return genericSucc("unmount", dir, estr)
	}

	return genericSucc("unmount", dir, "Unmount ok.")
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

func failure(caller, inp, msg string) error {
	resp, err := json.Marshal(&Resp{Status: "Failure", Message: msg})
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
	_ = logWrt.Warning(op)
}
