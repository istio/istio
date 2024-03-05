package ebpf

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/exp/constraints"
	"golang.org/x/sys/unix"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/nodeagent/constants"
	"istio.io/istio/pkg/log"
)

//go:generate ./gen.sh

const (
	BindV4ProgName = "bind_v4_prog"
	BindV6ProgName = "bind_v6_prog"
	LinkFolder     = "istio-ztunnel"
)

type Loader struct {
	baseBpfDir string
	cgroupPath string
}

func NewLoader(baseBpfDir, cgroupPath string) (*Loader, error) {
	if baseBpfDir == "" {
		var err error
		baseBpfDir, err = detectBpfFsPath()
		if err != nil {
			return nil, fmt.Errorf("detect bpf fs path: %w", err)
		}
	}

	baseBpfDir = filepath.Join(baseBpfDir, LinkFolder)
	if err := os.MkdirAll(baseBpfDir, 0755); err != nil {
		return nil, fmt.Errorf("create link root: %w", err)
	}

	if cgroupPath == "" {
		var err error
		cgroupPath, err = detectCgroupPath()
		if err != nil {
			return nil, fmt.Errorf("detect cgroup path: %w", err)
		}
	}

	return &Loader{
		baseBpfDir: baseBpfDir,
		cgroupPath: cgroupPath,
	}, nil
}

func (l *Loader) LoadPrograms() error {
	if err := rlimit.RemoveMemlock(); err != nil {
		return err
	}
	spec, err := LoadSpec(iptables.InpodMark, iptables.InpodMask, constants.DNSCapturePort, constants.ZtunnelInboundPort, constants.ZtunnelOutboundPort, constants.ZtunnelInboundPlaintextPort)
	if err != nil {
		return err
	}
	coll, err := ebpf.NewCollectionWithOptions(spec, ebpf.CollectionOptions{
		Maps: ebpf.MapOptions{
			PinPath: l.baseBpfDir,
		},
		Programs: ebpf.ProgramOptions{
			LogSize:  1024 * 1024 * 4,
			LogLevel: ebpf.LogLevelInstruction,
		}})
	if err != nil {
		if log.DebugEnabled() {
			var verr *ebpf.VerifierError
			if errors.As(err, &verr) {
				log.Debug(strings.Join(verr.Log, "\n"))
			}
		}
		return fmt.Errorf("loading objects: %w", err)
	}
	defer coll.Close()
	programV4 := coll.Programs[BindV4ProgName]
	programV6 := coll.Programs[BindV6ProgName]

	if programV4 == nil && programV6 == nil {
		return fmt.Errorf("no programs")
	}

	return l.loadProgs(programV4, programV6)
}

type Programs struct {
	BindV4Prog *ebpf.ProgramSpec
	BindV6Prog *ebpf.ProgramSpec
}

func LoadSpec(mark uint32, mask uint32, outport, inport, inplainport, dnsport uint16) (*ebpf.CollectionSpec, error) {
	coll, err := loadBlock_ztunnel_ports()
	if err != nil {
		return nil, err
	}

	rodata := coll.Maps[".rodata"]
	if rodata == nil || rodata.Value == nil {
		return nil, fmt.Errorf("no rodata")
	}
	dataSec, ok := rodata.Value.(*btf.Datasec)
	if !ok {
		return nil, fmt.Errorf("no rodata section vars")
	}

	if len(rodata.Contents) != 1 {
		return nil, fmt.Errorf("no rodata contents")
	}

	anyData := rodata.Contents[0].Value
	if anyData == nil {
		return nil, fmt.Errorf("no rodata section vars")
	}
	data, ok := anyData.([]byte)
	if !ok {
		return nil, fmt.Errorf("no rodata section vars")
	}
	err = nil

	err = errors.Join(err, set(data, dataSec.Vars, "ztunnel_mark", mark))
	err = errors.Join(err, set(data, dataSec.Vars, "ztunnel_mask", mask))
	err = errors.Join(err, set(data, dataSec.Vars, "ztunnel_outbound_port", outport))
	err = errors.Join(err, set(data, dataSec.Vars, "ztunnel_inbound_port", inport))
	err = errors.Join(err, set(data, dataSec.Vars, "ztunnel_inbound_plain_port", inplainport))
	err = errors.Join(err, set(data, dataSec.Vars, "ztunnel_dns_port", dnsport))
	if err != nil {
		return nil, err
	}

	return coll, nil
}

func GetProgsFromSpec(coll *ebpf.CollectionSpec) *Programs {
	programV4 := coll.Programs["bind_v4_prog"]
	programV6 := coll.Programs["bind_v6_prog"]

	return &Programs{
		BindV4Prog: programV4,
		BindV6Prog: programV6,
	}
}

func set[T constraints.Integer](contents []byte, vars []btf.VarSecinfo, name string, value T) error {
	for _, v := range vars {
		if vt, ok := v.Type.(*btf.Var); ok {
			if vt.Name == name {
				if uintptr(v.Size) != reflect.TypeOf(value).Size() {
					return fmt.Errorf("type mismatch")
				}
				buf := contents[v.Offset:]
				if len(buf) < int(v.Size) {
					return fmt.Errorf("not enough space")
				}

				for i := 0; i < int(v.Size); i++ {
					buf[i] = byte(value)
					value >>= 8
				}

				return nil
			}
		}
	}

	return fmt.Errorf("no such var %s", name)
}

func (l *Loader) loadProgs(programV4, programV6 *ebpf.Program) error {

	f, err := os.Open(l.cgroupPath)
	if err != nil {
		return fmt.Errorf("open cgroup: %w", err)
	}
	defer f.Close()

	err = l.loadProg(f, "bind4", programV4, ebpf.AttachCGroupInet4Bind)
	if err != nil {
		return err
	}

	// TODO: will this fail if ip v6 is not enabled?
	err = l.loadProg(f, "bind6", programV6, ebpf.AttachCGroupInet6Bind)
	if err != nil {
		return err
	}

	return nil
}

// logic taken from unexported "attachCgroup" in cilium's pkg/socketlb/cgroup.go

func (l *Loader) loadProg(cgroup *os.File, name string, prog *ebpf.Program, attach ebpf.AttachType) error {

	pin := filepath.Join(l.baseBpfDir, name)

	// Attempt to open and update an existing link.
	err := updateLink(pin, prog)
	switch {
	// Update successful, nothing left to do.
	case err == nil:
		log.Infof("Updated link %s for program %s", pin, name)

		return nil

	// Link exists, but is defunct, and needs to be recreated against a new
	// cgroup. This can happen in environments like dind where we're attaching
	// to a sub-cgroup that goes away if the container is destroyed, but the
	// link persists in the host's /sys/fs/bpf. The program no longer gets
	// triggered at this point and the link needs to be removed to proceed.
	case errors.Is(err, unix.ENOLINK):
		if err := os.Remove(pin); err != nil {
			return fmt.Errorf("unpinning defunct link %s: %w", pin, err)
		}

		log.Infof("Unpinned defunct link %s for program %s", pin, name)

	// No existing link found, continue trying to create one.
	case errors.Is(err, os.ErrNotExist):
		log.Infof("No existing link found at %s for program %s", pin, name)

	default:
		return fmt.Errorf("updating link %s for program %s: %w", pin, name, err)
	}

	link, err := link.AttachRawLink(link.RawLinkOptions{
		Target:  int(cgroup.Fd()),
		Program: prog,
		Attach:  attach,
	})
	if err != nil {
		return fmt.Errorf("attach program %s: %w", name, err)
	}
	defer link.Close()
	if err := link.Pin(pin); err != nil {
		return fmt.Errorf("pin link at %s for program %s : %w", pin, name, err)
	}

	return nil
}

func updateLink(pin string, prog *ebpf.Program) error {
	l, err := link.LoadPinnedLink(pin, &ebpf.LoadPinOptions{})
	if err != nil {
		return fmt.Errorf("opening pinned link %s: %w", pin, err)
	}
	defer l.Close()

	if err = l.Update(prog); err != nil {
		return fmt.Errorf("updating link %s: %w", pin, err)
	}
	return nil
}

func detectCgroupPath() (string, error) {
	return detectPathByType("cgroup2")
}

func detectBpfFsPath() (string, error) {
	return detectPathByType("bpf")
}

func detectPathByType(typ string) (string, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), " ")
		if len(fields) >= 3 && fields[2] == typ {
			return fields[1], nil
		}
	}

	return "", fmt.Errorf("%s not mounted", typ)
}
