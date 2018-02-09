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

	"github.com/spf13/cobra"

	"istio.io/istio/security/cmd/flexvolume/driver"
)

const (
	VER       string = "0.1"
	SYSLOGTAG string = "FlexVolNodeAgent"
)

var (
	rootCmd = &cobra.Command{
		Use:           "flexvoldrv",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	initCmd = &cobra.Command{
		Use:   "init",
		Short: "Flex volume init command.",
		Long:  "Flex volume init command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("init takes no arguments.")
			}

			// Absorb the error from the driver. The failure is indicated to kubelet via stdout.
			driver.InitCommand()
			return nil
		},
	}

	mountCmd = &cobra.Command{
		Use:   "mount",
		Short: "Flex volume unmount command.",
		Long:  "Flex volume unmount command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("mount takes 2 args.")
			}

			// Absorb the error from the driver. The failure is indicated to kubelet via stdout.
			driver.Mount(args[0], args[1])
			return nil
		},
	}

	unmountCmd = &cobra.Command{
		Use:   "unmount",
		Short: "Flex volume unmount command.",
		Long:  "Flex volume unmount command.",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("mount takes 1 args.")
			}

			// Absorb the error from the driver. The failure is indicated to kubelet via stdout.
			driver.Unmount(args[0])
			return nil
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Long:  "Flex volume driver version",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Printf("Version is %s\n", VER)
			return nil
		},
	}
)

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(mountCmd)
	rootCmd.AddCommand(unmountCmd)
}

func main() {
	driver.InitConfiguration()

	logWriter, err := driver.InitLog(SYSLOGTAG)
	if err != nil {
		log.Fatal(err)
	}
	defer logWriter.Close()

	if err = rootCmd.Execute(); err != nil {
		driver.GenericUnsupported("not supported", "", err.Error())
	}
}
