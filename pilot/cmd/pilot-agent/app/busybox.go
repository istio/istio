package app

import (
	"fmt"
	"os"
	"time"
)

func RunBusybox() {
	switch os.Args[0] {
	case "/usr/bin/sleep", "sleep":
		if len(os.Args) < 2 {
			fmt.Fprintln(os.Stderr, "sleep: missing operand")
			os.Exit(1)
		}
		dur, _ := time.ParseDuration(os.Args[1])
		time.Sleep(dur)
case "/usr/bin/sh", "sh":
	fmt.Fprintln(os.Stderr, "Istio is running in distroless mode and does not have a shell. See https://istio.io/latest/docs/ops/configuration/security/harden-docker-images/ for more information.")
	os.Exit(1)
	}
}
