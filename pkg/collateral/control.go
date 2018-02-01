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

package collateral

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/pflag"
)

// Control determines the behavior of the EmitCollateral function
type Control struct {
	// OutputDir specifies the directory to output the collateral files
	OutputDir string

	// EmitManPages controls whether to produce man pages.
	EmitManPages bool

	// EmitYAML controls whether to produce YAML files.
	EmitYAML bool

	// EmitBashCompletion controls whether to produce bash completion files.
	EmitBashCompletion bool

	// EmitMarkdown controls whether to produce mankdown documentation files.
	EmitMarkdown bool

	// ManPageInfo provides extra information necessary when emitting man pages.
	ManPageInfo doc.GenManHeader
}

// EmitCollateral produces a set of collateral files for a CLI command. You can
// select to emit markdown to describe a command's function, man pages, YAML
// descriptions, and bash completion files.
func EmitCollateral(root *cobra.Command, c *Control) error {
	if c.EmitManPages {
		if err := doc.GenManTree(root, &c.ManPageInfo, c.OutputDir); err != nil {
			return fmt.Errorf("unable to output manpage tree: %v", err)
		}
	}

	if c.EmitMarkdown {
		if err := genMarkdown(root, c.OutputDir+"/"+root.Name()+".md"); err != nil {
			return fmt.Errorf("unable to output markdown tree: %v", err)
		}
	}

	if c.EmitYAML {
		if err := doc.GenYamlTree(root, c.OutputDir); err != nil {
			return fmt.Errorf("unable to output YAML tree: %v", err)
		}
	}

	if c.EmitBashCompletion {
		if err := root.GenBashCompletionFile(c.OutputDir + "/" + root.Name() + ".bash"); err != nil {
			return fmt.Errorf("unable to output bash completion file: %v", err)
		}
	}

	return nil
}

func genMarkdown(cmd *cobra.Command, path string) error {
	commands := make(map[string]*cobra.Command)
	findCommands(commands, cmd)
	names := make([]string, len(commands), len(commands))
	i := 0
	for n := range commands {
		names[i] = n
		i++
	}
	sort.Strings(names)

	buf := &bytes.Buffer{}
	genFileHeader(cmd, buf)
	for _, n := range names {
		if commands[n].Name() == "help" {
			continue
		}

		genCommand(commands[n], buf)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = f.WriteString(buf.String())
	_ = f.Close()

	return err
}

func findCommands(commands map[string]*cobra.Command, cmd *cobra.Command) {
	cmd.InitDefaultHelpCmd()
	cmd.InitDefaultHelpFlag()

	commands[cmd.CommandPath()] = cmd
	for _, c := range cmd.Commands() {
		findCommands(commands, c)
	}
}

func genFileHeader(cmd *cobra.Command, b *bytes.Buffer) {
	_, _ = b.WriteString("---\n")
	_, _ = b.WriteString("title: " + cmd.Name() + "\n")
	_, _ = b.WriteString("overview: " + cmd.Short + "\n")
	_, _ = b.WriteString("layout: docs\n")
	_, _ = b.WriteString("---\n")
}

func genCommand(cmd *cobra.Command, b *bytes.Buffer) {
	if cmd.Hidden || cmd.Deprecated != "" {
		return
	}

	if cmd.HasParent() {
		b.WriteString("\n## " + cmd.CommandPath() + "\n\n")
	}

	if cmd.Long != "" {
		b.WriteString(cmd.Long + "\n")
	} else if cmd.Short != "" {
		b.WriteString(cmd.Short + "\n")
	}

	if cmd.Runnable() {
		fmt.Fprintf(b, "```bash\n%s\n```\n\n", cmd.UseLine())
	}

	// TODO: output aliases

	flags := cmd.NonInheritedFlags()
	flags.SetOutput(b)

	parentFlags := cmd.InheritedFlags()
	parentFlags.SetOutput(b)

	if flags.HasFlags() || parentFlags.HasFlags() {
		fmt.Fprintf(b, "\n")
		fmt.Fprintf(b, "|Option|Shorthand|Description\n")
		fmt.Fprintf(b, "|------|---------|-----------\n")

		lines := flagUsages(b, flags)
		lines = append(lines, flagUsages(b, parentFlags)...)

		sort.Strings(lines)
		for _, l := range lines {
			fmt.Fprintf(b, l+"\n")
		}
		fmt.Fprintf(b, "\n")
	}

	if len(cmd.Example) > 0 {
		fmt.Fprintf(b, "\n")
		fmt.Fprintf(b, "### Examples\n\n")
		fmt.Fprintf(b, "```bash\n%s\n```\n\n", cmd.Example)
	}
}

func flagUsages(b *bytes.Buffer, f *pflag.FlagSet) []string {
	var lines []string

	f.VisitAll(func(flag *pflag.Flag) {
		if flag.Deprecated != "" || flag.Hidden {
			return
		}

		if flag.Name == "help" {
			return
		}

		varname, usage := unquoteUsage(flag)
		if varname != "" {
			varname = " <" + varname + ">"
		}

		def := ""
		if flag.Value.Type() == "string" {
			def = fmt.Sprintf(" (default `%q`)", flag.DefValue)
		} else if flag.Value.Type() != "bool" {
			def = fmt.Sprintf(" (default `%s`)", flag.DefValue)
		}

		if flag.Shorthand != "" && flag.ShorthandDeprecated == "" {
			lines = append(lines, fmt.Sprintf("|`--%s%s`|`-%s`|%s %s", flag.Name, varname, flag.Shorthand, usage, def))
		} else {
			lines = append(lines, fmt.Sprintf("|`--%s%s`||%s %s", flag.Name, varname, usage, def))
		}
	})

	return lines
}

// unquoteUsage extracts a back-quoted name from the usage
// string for a flag and returns it and the un-quoted usage.
// Given "a `name` to show" it returns ("name", "a name to show").
// If there are no back quotes, the name is an educated guess of the
// type of the flag's value, or the empty string if the flag is boolean.
func unquoteUsage(flag *pflag.Flag) (name string, usage string) {
	// Look for a back-quoted name, but avoid the strings package.
	usage = flag.Usage
	for i := 0; i < len(usage); i++ {
		if usage[i] == '`' {
			for j := i + 1; j < len(usage); j++ {
				if usage[j] == '`' {
					name = usage[i+1 : j]
					usage = usage[:i] + name + usage[j+1:]
					return name, usage
				}
			}
			break // Only one back quote; use type name.
		}
	}

	name = flag.Value.Type()
	switch name {
	case "bool":
		name = ""
	case "float64":
		name = "float"
	case "int64":
		name = "int"
	case "uint64":
		name = "uint"
	}

	return
}
