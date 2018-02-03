// Flexvolume driver that is invoked by kubelet when a pod installs a flexvolume drive
// of type nodeagent/uds
// This driver communicates to the nodeagent/idagent using protos/nodeagementmgmt.proto
// and shares the properties of the pod with nodeagent/idagent.
//
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/syslog"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	nagent "github.com/wattli/proto-udsuspver/nodeagentmgmt"
	pb "github.com/wattli/proto-udsuspver/protos/mgmtintf_v1"
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
	Capabilities *DriverCapabilities `json:",omitempty"`
}

type DriverCapabilities struct {
	Attach         bool `json:"attach"`
	SELinuxRelabel bool `json:"selinuxRelabel"`
}

type NodeAgentInputs struct {
	Uid            string `json:"kubernetes.io/pod.uid"`
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

var (
	logWrt *syslog.Writer

	RootCmd = &cobra.Command{
		Use:   "flexvoldrv",
		Short: "Flex volume driver interface for Node Agent.",
		Long:  "Flex volume driver interface for Node Agent.",
	}

	InitCmd = &cobra.Command{
		Use:   "init",
		Short: "Flex volume init command.",
		Long:  "Flex volume init command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("init takes no arguments.")
			}
			return Init()
		},
	}

	AttachCmd = &cobra.Command{
		Use:   "attach",
		Short: "Flex volumen attach command.",
		Long:  "Flex volumen attach command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return fmt.Errorf("attach takes at most 2 args.")
			}
			return Attach(args[0], args[1])
		},
	}

	DetachCmd = &cobra.Command{
		Use:   "detach",
		Short: "Flex volume detach command.",
		Long:  "Flex volume detach command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("detach takes at least 1 arg.")
			}
			return Detach(args[0])
		},
	}

	WaitAttachCmd = &cobra.Command{
		Use:   "waitforattach",
		Short: "Flex volume waitforattach command.",
		Long:  "Flex volume waitforattach command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("waitforattach takes at least 2 arg.")
			}
			return WaitAttach(args[0], args[1])
		},
	}

	IsAttachedCmd = &cobra.Command{
		Use:   "isattached",
		Short: "Flex volume isattached command.",
		Long:  "Flex volume isattached command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("isattached takes at least 2 arg.")
			}
			return IsAttached(args[0], args[1])
		},
	}

	MountDevCmd = &cobra.Command{
		Use:   "mountdevice",
		Short: "Flex volume unmount command.",
		Long:  "Flex volume unmount command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 3 {
				return fmt.Errorf("mountdevice takes 3 args.")
			}
			return MountDev(args[0], args[1], args[2])
		},
	}

	UnmountDevCmd = &cobra.Command{
		Use:   "unmountdevice",
		Short: "Flex volume unmount command.",
		Long:  "Flex volume unmount command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("unmountdevice takes 1 arg.")
			}
			return UnmountDev(args[0])
		},
	}

	MountCmd = &cobra.Command{
		Use:   "mount",
		Short: "Flex volume unmount command.",
		Long:  "Flex volume unmount command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("mount takes 2 args.")
			}
			return Mount(args[0], args[1])
		},
	}

	UnmountCmd = &cobra.Command{
		Use:   "unmount",
		Short: "Flex volume unmount command.",
		Long:  "Flex volume unmount command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("mount takes 1 args.")
			}
			return Unmount(args[0])
		},
	}

	GetVolNameCmd = &cobra.Command{
		Use:   "getvolumename",
		Short: "Flex volume getvolumename command.",
		Long:  "Flex volume getvolumename command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("mount takes 1 args.")
			}
			return GetVolName(args[0])
		},
	}
)

func getNewVolume() string {
	return volumeName
}

func Init() error {
	if ver == "1.8" {
		resp, err := json.Marshal(&Resp{Status: "Success", Message: "Init ok.", Capabilities: &DriverCapabilities{Attach: false}})
		if err != nil {
			return err
		}
		fmt.Println(string(resp))
		return nil
	}
	return genericSucc("init", "", "Init ok.")
}

func Attach(opts, nodeName string) error {
	devId := getNewVolume()
	resp, err := json.Marshal(&Resp{Device: devId, Status: "Success", Message: "Dir created"})
	if err != nil {
		return err
	}
	fmt.Println(string(resp))
	inp := opts + "|" + nodeName
	logToSys("attach", inp, string(resp))
	return nil
}

func Detach(devId string) error {
	resp, err := json.Marshal(&Resp{Status: "Success", Message: "Gone " + devId})
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Println(string(resp))
	logToSys("detach", devId, string(resp))
	return nil
}

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

func MountDev(dir, dev, opts string) error {
	inp := dir + "|" + dev + "|" + opts
	return genericSucc("mountdev", inp, "Mount dev ok.")
}

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

	wlInfo := pb.WorkloadInfo{Uid: ninputs.Uid,
		Workload:       ninputs.Name,
		Namespace:      ninputs.Namespace,
		Serviceaccount: ninputs.ServiceAccount}
	return &wlInfo, true
}

func doMount(dstDir string, ninputs *pb.WorkloadInfo) error {
	newDir := NodeAgentUdsHome + "/" + ninputs.Uid
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

func doUnmount(dir string) error {
	cmd := exec.Command("/bin/umount", dir)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func Mount(dir, opts string) error {
	inp := dir + "|" + opts

	ninputs, s := checkValidMountOpts(opts)
	if s == false {
		return Failure("mount", inp, "Incomplete inputs")
	}

	if err := doMount(dir, ninputs); err != nil {
		sErr := "Failure to mount: " + err.Error()
		return Failure("mount", inp, sErr)
	}

	if err := AddListener(ninputs); err != nil {
		sErr := "Failure to notify nodeagent: " + err.Error()
		return Failure("mount", inp, sErr)
	}

	return genericSucc("mount", inp, "Mount ok.")
}

func Unmount(dir string) error {
	// Stop the listener.
	// /var/lib/kubelet/pods/20154c76-bf4e-11e7-8a7e-080027631ab3/volumes/nodeagent~uds/test-volume/
	// /var/lib/kubelet/pods/2dc75e9a-cbec-11e7-b158-0800270da466/volumes/nodeagent~uds/test-volume
	comps := strings.Split(dir, "/")
	if len(comps) < 6 {
		sErr := fmt.Sprintf("Failure to notify nodeagent dir %v", dir)
		return Failure("unmount", dir, sErr)
	}

	uid := comps[5]
	// TBD: Check if uid is the correct format.
	naInp := &pb.WorkloadInfo{Uid: uid}
	if err := DelListener(naInp); err != nil {
		sErr := "Failure to notify nodeagent: " + err.Error()
		return Failure("unmount", dir, sErr)
	}

	// unmount the bind mount
	doUnmount(dir + "/nodeagent")
	// unmount the tmpfs
	doUnmount(dir)
	// delete the directory that was created.
	delDir := NodeAgentUdsHome + "/" + uid
	err := os.Remove(delDir)
	if err != nil {
		estr := fmt.Sprintf("unmount del failure %s: %s", delDir, err.Error())
		return genericSucc("unmount", dir, estr)
	}

	return genericSucc("unmount", dir, "Unmount ok.")
}

func GetVolName(opts string) error {
	if ver == "1.8" {
		return genericUnsupported("getvolname", opts, "not supported")
	}

	devName := getNewVolume()
	resp, err := json.Marshal(&Resp{VolumeName: devName, Status: "Success", Message: "ok"})
	if err != nil {
		return err
	}

	sResp := string(resp)
	fmt.Println(sResp)
	logToSys("getvolname", opts, sResp)
	return nil
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

func AddListener(ninputs *pb.WorkloadInfo) error {
	client := nagent.ClientUds(NodeAgentMgmtAPI)
	if client == nil {
		return errors.New("Failed to create Nodeagent client.")
	}

	_, err := client.WorkloadAdded(ninputs)
	if err != nil {
		return err
	}

	client.Close()

	return nil
}

func DelListener(ninputs *pb.WorkloadInfo) error {
	client := nagent.ClientUds(NodeAgentMgmtAPI)
	if client == nil {
		return errors.New("Failed to create Nodeagent client.")
	}

	_, err := client.WorkloadDeleted(ninputs)
	if err != nil {
		return err
	}

	client.Close()
	return nil
}

func init() {
	RootCmd.AddCommand(InitCmd)
	RootCmd.AddCommand(AttachCmd)
	RootCmd.AddCommand(DetachCmd)
	RootCmd.AddCommand(WaitAttachCmd)
	RootCmd.AddCommand(IsAttachedCmd)
	RootCmd.AddCommand(MountDevCmd)
	RootCmd.AddCommand(UnmountDevCmd)
	RootCmd.AddCommand(MountCmd)
	RootCmd.AddCommand(UnmountCmd)
	RootCmd.AddCommand(GetVolNameCmd)
}

func main() {
	var err error
	logWrt, err = syslog.New(syslog.LOG_WARNING|syslog.LOG_DAEMON, "FlexVolNodeAgent")
	if err != nil {
		log.Fatal(err)
	}
	defer logWrt.Close()

	if logWrt == nil {
		fmt.Println("am Logwrt is nil")
	}
	if err = RootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
