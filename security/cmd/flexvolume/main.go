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

package main

import (
	"fmt"
	"log"
	"log/syslog"

	"github.com/spf13/cobra"

	"istio.io/istio/security/cmd/flexvolume/driver"
)

const (
	// TODO(wattli): make it configurable.
	ver string = "1.8"
)

var (
	logWrt *syslog.Writer

	// RootCmd defines the root command for the driver.
	RootCmd = &cobra.Command{
		Use:   "flexvoldrv",
		Short: "Flex volume driver interface for Node Agent.",
		Long:  "Flex volume driver interface for Node Agent.",
	}

	// InitCmd defines the init command for the driver.
	InitCmd = &cobra.Command{
		Use:   "init",
		Short: "Flex volume init command.",
		Long:  "Flex volume init command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("init takes no arguments")
			}
			return driver.Init(ver)
		},
	}

	// MountCmd defines the mount command
	MountCmd = &cobra.Command{
		Use:   "mount",
		Short: "Flex volume unmount command.",
		Long:  "Flex volume unmount command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("mount takes 2 args")
			}
			return driver.Mount(args[0], args[1])
		},
	}

	// UnmountCmd defines the unmount command
	UnmountCmd = &cobra.Command{
		Use:   "unmount",
		Short: "Flex volume unmount command.",
		Long:  "Flex volume unmount command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("mount takes 1 args")
			}
			return driver.Unmount(args[0])
		},
	}
)

func init() {
	RootCmd.AddCommand(InitCmd)
	RootCmd.AddCommand(MountCmd)
	RootCmd.AddCommand(UnmountCmd)
}

func main() {
	var err error
	logWrt, err = syslog.New(syslog.LOG_WARNING|syslog.LOG_DAEMON, "FlexVolNodeAgent")
	if err != nil {
		log.Fatal(err)
	}
	defer logWrt.Close() // nolint: errcheck

	if logWrt == nil {
		fmt.Println("am Logwrt is nil")
	}
	if err = RootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
