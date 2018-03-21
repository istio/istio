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

package util

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"
)

// IsProcessRunning check if a os.Process is running
func IsProcessRunning(p *os.Process) (bool, error) {
	if p == nil {
		return false, nil
	}
	err := p.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	return false, fmt.Errorf("process %d is not alive or not belongs to you %v", p.Pid, err)
}

// IsProcessRunningInt check if a process of the given pid(int) is running
func IsProcessRunningInt(pid int) (bool, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Errorf("failed to find process %d: %v (check os.FindProcess)", pid, err)
	}
	return IsProcessRunning(process)
}

// IsProcessRunningString check if a process of the given pid(string) is running
func IsProcessRunningString(pidS string) (bool, error) {
	pid, err := strconv.Atoi(pidS)
	if err != nil {
		return false, fmt.Errorf("can't covert %s to int: %v", pidS, err)
	}
	return IsProcessRunningInt(pid)
}

// KillProcess kill a os.Process
func KillProcess(p *os.Process) (err error) {
	var alive bool
	if alive, err = IsProcessRunning(p); !alive {
		log.Printf("Skip stop process %d: %v", p.Pid, err)
		return nil
	}

	if err = p.Kill(); err != nil {
		return fmt.Errorf("failed to kill process %d: %v", p.Pid, err)
	}
	if err = p.Release(); err != nil {
		return fmt.Errorf("failed to release resource of process %d: %v", p.Pid, err)
	}

	return nil
}
