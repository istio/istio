package ebpf_test

import (
	"errors"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"

	cebpf "github.com/cilium/ebpf"
	"github.com/containerd/cgroups/v3/cgroup2"
	"istio.io/istio/cni/pkg/ebpf"
	"istio.io/istio/cni/pkg/iptables"
)

func TestLoadPrograms(t *testing.T) {

	l, err := ebpf.NewLoader("")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.LoadPrograms(); err != nil {
		var verr *cebpf.VerifierError
		if errors.As(err, &verr) {
			t.Log(strings.Join(verr.Log, "\n"))
		}
		t.Fatal(err)
	}

	defer os.RemoveAll(l.BaseBpfDir)

	// create a new cgroup and run a goroutine in it to test blocking ports
	testZtunnel(t)

}

func testZtunnel(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// move into cgroup
	res := cgroup2.Resources{}
	// dummy PID of -1 is used for creating a "general slice" to be used as a parent cgroup.
	// see https://github.com/containerd/cgroups/blob/1df78138f1e1e6ee593db155c6b369466f577651/v2/manager.go#L732-L735
	c, err := cgroup2.NewSystemd("/", "ztunnel-test-cgroup.slice", -1, &res)

	//	cg, err := ebpf.DetectPathByType("cgroup2")
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//	c, err := cgroup2.NewManager(cg, "ztunnel-cgroup", &res)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Delete()
	err = c.AddProc(uint64(syscall.Gettid()))
	if err != nil {
		t.Fatal(err)
	}

	bindZtunnel(t)

	// now just tcp listener on the ztunnel port
	// this should fail
	l, err := net.Listen("tcp", ":15008")
	if err == nil {
		l.Close()
		t.Fatal("expected error")
	}

	// make sure that the error is permission denied
	if !errors.Is(err, syscall.EPERM) {
		t.Fatal(err)
	}

}

func bindZtunnel(t *testing.T) {

	// create socket
	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(s)

	// set mark
	err = syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_MARK, iptables.InpodMark)
	if err != nil {
		t.Fatal(err)
	}

	// set addr re-use
	err = syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	if err != nil {
		t.Fatal(err)
	}

	// bind
	err = syscall.Bind(s, &syscall.SockaddrInet4{Port: 15008})
}
