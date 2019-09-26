// Copyright 2019 Istio Authors
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

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/pkg/log"
)

// Level is an enumeration of all supported log levels.
type Level int

const (
	DefaultLoggerName   = "level"
	defaultOutputLevel = WarningLevel
)

const (
	// OffLevel disables logging
	OffLevel Level = iota
	// CriticalLevel enables critical level logging
	CriticalLevel
	// ErrorLevel enables error level logging
	ErrorLevel
	// WarningLevel enables warning level logging
	WarningLevel
	// InfoLevel enables info level logging
	InfoLevel
	// DebugLevel enables debug level logging
	DebugLevel
	// TraceLevel enables trace level logging
	TraceLevel
)

// existing sorted active loggers
var activeLoggers = []string{
	"admin",
	"all",
	"aws",
	"assert",
	"backtrace",
	"client",
	"config",
	"connection",
	"dubbo",
	"file",
	"filter",
	"forward_proxy",
	"grpc",
	"hc",
	"health_checker",
	"http",
	"http2",
	"hystrix",
	"init",
	"io",
	"jwt",
	"kafka",
	"lua",
	"main",
	"misc",
	"mongo",
	"quic",
	"pool",
	"rbac",
	"redis",
	"router",
	"runtime",
	"stats",
	"secret",
	"tap",
	"testing",
	"thrift",
	"tracing",
	"upstream",
	"udp",
	"wasm",
}

var levelToString = map[Level]string{
	TraceLevel:    "trace",
	DebugLevel:    "debug",
	InfoLevel:     "info",
	WarningLevel:  "warning",
	ErrorLevel:    "error",
	CriticalLevel: "critical",
	OffLevel:      "off",
}

var stringToLevel = map[string]Level{
	"trace":    TraceLevel,
	"debug":    DebugLevel,
	"info":     InfoLevel,
	"warning":  WarningLevel,
	"error":    ErrorLevel,
	"critical": CriticalLevel,
	"off":      OffLevel,
}

var (
	logLevelString = ""
	follow         = false
	tail           = -1
	reset          = false
)

func pod() *cobra.Command {
	podCmd := &cobra.Command{
		Use:     "pod",
		Aliases: []string{"p"},
		Short:   "Update logging level of istio-proxy in the specified pod, and get logs stream by optional.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("specify a pod")
			}

			destLoggerLevels := map[string]Level{}
			if reset {
				// reset logging level to `defaultOutputLevel`, and ignore the `--log_level` option
				destLoggerLevels[DefaultLoggerName] = defaultOutputLevel
			} else if logLevelString != "" {
				// parse `logLevelString` and update logging level of envoy
				levels := strings.Split(logLevelString, ",")
				for _, ol := range levels {
					if !strings.Contains(ol, ":") || strings.Index(ol, "all:") == 0 {
						level, ok := stringToLevel[ol[strings.Index(ol, ":")+1:]]
						if ok {
							destLoggerLevels = map[string]Level{
								DefaultLoggerName: level,
							}
							break
						}
					} else {
						loggerLevel := strings.Split(ol, ":")
						if len(loggerLevel) != 2 {
							break
						}

						index := sort.SearchStrings(activeLoggers, loggerLevel[0])
						ok1 := index < len(activeLoggers) && activeLoggers[index] == loggerLevel[0]
						level, ok2 := stringToLevel[loggerLevel[1]]
						if !ok1 || !ok2 {
							break
						}
						destLoggerLevels[loggerLevel[0]] = level
					}
				}
			}

			podName, ns := handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			fw, err := client.BuildPortForwarder(podName, ns, 0, 15000)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for %s: %v", podName, err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				log.Debugf("port-forward to envoy sidecar ready")

				// traversing all keys in map `destLoggers`, and parse & print the last response
				var resp *http.Response
				var err error
				if len(destLoggerLevels) == 0 {
					resp, err = http.Post(fmt.Sprintf("http://localhost:%d/logging", fw.LocalPort),
						"application/json", bytes.NewBufferString(""))
				} else {
					for l, level := range destLoggerLevels {
						resp, err = http.Post(fmt.Sprintf("http://localhost:%d/logging?%s=%s", fw.LocalPort, l, levelToString[level]),
							"application/json", bytes.NewBufferString(""))
					}
				}
				if err != nil {
					return fmt.Errorf("failure sending http post request to set log level of envoy sidecar")
				}
				defer resp.Body.Close()

				var htmlData []byte
				htmlData, err = ioutil.ReadAll(resp.Body)
				if err != nil {
					return fmt.Errorf("failure getting http post response body")
				}
				_, _ = fmt.Fprint(cmd.OutOrStdout(), string(htmlData))

				close(fw.StopChannel)
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}

			if follow {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), "====\n")
				if tail < 0 {
					execCommand("kubectl", cmd.OutOrStdout(), cmd.OutOrStderr(), "logs", "-f", podName, "-c", "istio-proxy")
				} else {
					execCommand("kubectl", cmd.OutOrStdout(), cmd.OutOrStderr(), "logs", "-f",
						fmt.Sprintf("--tail=%v", tail), podName, "-c", "istio-proxy")
				}
			}
			return nil
		},
	}

	levelListString := fmt.Sprintf("[%s, %s, %s, %s]",
		levelToString[DebugLevel],
		levelToString[InfoLevel],
		levelToString[WarningLevel],
		levelToString[ErrorLevel])

	s := strings.Join(activeLoggers, ", ")

	podCmd.PersistentFlags().BoolVarP(&reset, "reset", "r", reset, "Specify if the reset logging level to default value (warning).")
	podCmd.PersistentFlags().StringVar(&logLevelString, "log_level", logLevelString,
		fmt.Sprintf("Comma-separated minimum per-logger level of messages to output, in the form of"+
			" <logger>:<level>,<logger>:<level>,... where logger can be one of %s and level can be one of %s",
			s, levelListString))
	podCmd.PersistentFlags().BoolVarP(&follow, "follow", "f", follow, "Specify if the logs should be streamed.")
	_ = podCmd.PersistentFlags().MarkHidden("follow")
	podCmd.PersistentFlags().IntVar(&tail, "tail", tail,
		"Lines of recent log file to display. Defaults to -1 showing all log lines.")
	_ = podCmd.PersistentFlags().MarkHidden("tail")

	return podCmd
}

func execCommand(name string, cmdStdout io.Writer, cmdStderr io.Writer, arg ...string) {
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := exec.Command(name, arg...)
	stdoutIn, _ := cmd.StdoutPipe()
	stderrIn, _ := cmd.StderrPipe()
	var errStdout, errStderr error
	stdout := io.MultiWriter(cmdStdout, &stdoutBuf)
	stderr := io.MultiWriter(cmdStderr, &stderrBuf)
	err := cmd.Start()
	if err != nil {
		log.Fatalf("cmd.Start() failed with '%s'\n", err)
	}
	go func() {
		_, errStdout = io.Copy(stdout, stdoutIn)
	}()
	go func() {
		_, errStderr = io.Copy(stderr, stderrIn)
	}()
	err = cmd.Wait()
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
	}
	if errStdout != nil || errStderr != nil {
		log.Fatal("failed to capture stdout or stderr\n")
	}
}
