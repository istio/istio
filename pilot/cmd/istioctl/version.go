package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"os/user"
)

func runVersionCmd(c *cobra.Command, args []string) error {
	usr, err := user.Current()
	if err != nil {
		return err
	}
	i, err := NewKubeInstallerFromLocalPath(usr.HomeDir)
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
