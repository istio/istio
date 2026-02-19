package nodeagent

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type Pidfd int

var (
	ErrPidfdNotSupported   = errors.New("pidfd open is not supported on this platform")
	ErrPidfdOpenFailed     = errors.New("pidfd open failed")
	ErrPidNotFoundInFdInfo = errors.New("pid not found in fdinfo")
	ErrPidNotInNS          = errors.New("pid is zero - process likely not available in pidns")
	ErrPidGone             = errors.New("invalid pid - process likely gone")
)

func PidFdOpen(pid int) (Pidfd, error) {
	if SYS_PIDFD_OPEN <= 0 {
		return -1, ErrPidfdNotSupported
	}
	pidfdFromSyscall, _, err := syscall.Syscall(SYS_PIDFD_OPEN, uintptr(pid), 0, 0)
	if err != 0 {
		return -1, err
	}
	pidfd := Pidfd(pidfdFromSyscall)
	if pidfd < 0 {
		return -1, ErrPidfdOpenFailed
	}
	return pidfd, nil
}

func (p Pidfd) Close() error {
	if p >= 0 {
		return syscall.Close(int(p))
	}
	return nil
}

func (p Pidfd) IsValid() bool {
	return p >= 0
}

func (p Pidfd) Pid() (int, error) {
	fdinfo := fmt.Sprintf("/proc/self/fdinfo/%d", int(p))
	data, err := os.ReadFile(fdinfo)
	if err != nil {
		return 0, err
	}
	return parsePidFromFdInfo(data)
}

func parsePidFromFdInfo(data []byte) (int, error) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Pid:\t") {
			pidStr := strings.TrimPrefix(line, "Pid:\t")
			pidInt, err := strconv.Atoi(pidStr)
			switch {
			case err != nil:
				return 0, fmt.Errorf("failed to parse pid from fdinfo: %w", err)
			case pidInt == 0:
				return 0, ErrPidNotInNS
			case pidInt < 0:
				return 0, ErrPidGone
			default:
				return pidInt, nil
			}
		}
	}
	return 0, ErrPidNotFoundInFdInfo
}
