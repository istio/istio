package main

import (
	"os"
)

func main() {
	rootCmd := getRootCmd(os.Args[1:])

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
