package main

import (
	"fmt"
	"github.com/spf13/cobra"
)

func runVersionCmd(c *cobra.Command, args []string) error {
	const testLocalPath = "/usr/local/google/home/yusuo"
	i, err := NewKubeInstallerFromLocalPath(testLocalPath)
	if err != nil {
		return err
	}
	version, err := i.Check()
	if err != nil {
		return err
	}
	fmt.Println(version)
	return nil
}

func init() {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Display version information",
		RunE:  runVersionCmd,
	}
	rootCmd.AddCommand(versionCmd)
}
