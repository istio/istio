// Copyright Istio Authors
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

// Package mesh contains types and functions.
package mesh

import (
	"fmt"
	"io"
	"strings"

	"istio.io/istio/pkg/log"
)

// installerScope is the scope for all commands in the mesh package.
var installerScope = log.RegisterScope("installer", "installer")

type Printer interface {
	Printf(format string, a ...any)
	Println(string)
}

func NewPrinterForWriter(w io.Writer) Printer {
	return &writerPrinter{writer: w}
}

type writerPrinter struct {
	writer io.Writer
}

func (w *writerPrinter) Printf(format string, a ...any) {
	_, _ = fmt.Fprintf(w.writer, format, a...)
}

func (w *writerPrinter) Println(str string) {
	_, _ = fmt.Fprintln(w.writer, str)
}

// Confirm waits for a user to confirm with the supplied message.
func Confirm(msg string, writer io.Writer) bool {
	for {
		_, _ = fmt.Fprintf(writer, "%s ", msg)
		var response string
		_, err := fmt.Scanln(&response)
		if err != nil {
			return false
		}
		switch strings.ToUpper(response) {
		case "Y", "YES":
			return true
		case "N", "NO":
			return false
		}
	}
}

// --manifests is an alias for --set installPackagePath=
// --revision is an alias for --set revision=
func applyFlagAliases(flags []string, manifestsPath, revision string) []string {
	if manifestsPath != "" {
		flags = append(flags, fmt.Sprintf("installPackagePath=%s", manifestsPath))
	}
	if revision != "" && revision != "default" {
		flags = append(flags, fmt.Sprintf("revision=%s", revision))
	}
	return flags
}
