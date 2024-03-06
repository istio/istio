//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package app

import "golang.org/x/sys/unix"

func getFDLimit() (uint64, error) {
	rlimit := unix.Rlimit{}
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &rlimit); err != nil {
		return 0, err
	}

	return rlimit.Cur, nil
}
