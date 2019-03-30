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
	"html"
	"os"
	"sort"
	"strconv"
	"strings"

	"istio.io/istio/pkg/annotations"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/pflag"

	"istio.io/istio/pkg/env"
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

	// EmitMarkdown controls whether to produce markdown documentation files.
	EmitMarkdown bool

	// EmitHTMLFragmentWithFrontMatter controls whether to produce HTML fragments with Jekyll/Hugo front matter.
	EmitHTMLFragmentWithFrontMatter bool

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
		if err := doc.GenMarkdownTree(root, c.OutputDir); err != nil {
			return fmt.Errorf("unable to output markdown tree: %v", err)
		}
	}

	if c.EmitHTMLFragmentWithFrontMatter {
		if err := genHTMLFragment(root, c.OutputDir+"/"+root.Name()+".html"); err != nil {
			return fmt.Errorf("unable to output HTML fragment file: %v", err)
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

type generator struct {
	buffer *bytes.Buffer
}

func (g *generator) emit(str ...string) {
	for _, s := range str {
		g.buffer.WriteString(s)
	}
	g.buffer.WriteByte('\n')
}

func findCommands(commands map[string]*cobra.Command, cmd *cobra.Command) {
	cmd.InitDefaultHelpCmd()
	cmd.InitDefaultHelpFlag()

	commands[cmd.CommandPath()] = cmd
	for _, c := range cmd.Commands() {
		findCommands(commands, c)
	}
}

const help = "help"

func genHTMLFragment(cmd *cobra.Command, path string) error {
	commands := make(map[string]*cobra.Command)
	findCommands(commands, cmd)

	names := make([]string, len(commands))
	i := 0
	for n := range commands {
		names[i] = n
		i++
	}
	sort.Strings(names)

	g := &generator{
		buffer: &bytes.Buffer{},
	}

	count := 0
	for _, n := range names {
		if commands[n].Name() == help {
			continue
		}

		count++
	}

	g.genFrontMatter(cmd, count)
	for _, n := range names {
		if commands[n].Name() == help {
			continue
		}

		g.genCommand(commands[n])
	}

	g.genVars(cmd)
	g.genAnnotations(cmd)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = g.buffer.WriteTo(f)
	_ = f.Close()

	return err
}

func (g *generator) genFrontMatter(root *cobra.Command, numEntries int) {
	g.emit("---")
	g.emit("title: ", root.Name())
	g.emit("description: ", root.Short)
	g.emit("generator: pkg-collateral-docs")
	g.emit("number_of_entries: ", strconv.Itoa(numEntries))
	g.emit("---")
}

func (g *generator) genCommand(cmd *cobra.Command) {
	if cmd.Hidden || cmd.Deprecated != "" {
		return
	}

	if cmd.HasParent() {
		g.emit("<h2 id=\"", normalizeID(cmd.CommandPath()), "\">", cmd.CommandPath(), "</h2>")
	}

	if cmd.Long != "" {
		g.emitText(cmd.Long)
	} else if cmd.Short != "" {
		g.emitText(cmd.Short)
	}

	if cmd.Runnable() {
		g.emit("<pre class=\"language-bash\"><code>", html.EscapeString(cmd.UseLine()))
		g.emit("</code></pre>")

		if len(cmd.Aliases) > 0 {
			// first word in cmd.Use represents the command that is being aliased
			word := cmd.Use
			index := strings.Index(word, " ")
			if index > 0 {
				word = word[0:index]
			}

			g.emit("<div class=\"aliases\">")
			line := cmd.UseLine()
			for i, alias := range cmd.Aliases {
				r := strings.Replace(line, word, alias, 1)
				if i == 0 {
					g.emit("<pre class=\"language-bash\"><code>", html.EscapeString(r))
				} else {
					g.emit(html.EscapeString(r))
				}
			}
			g.emit("</code></pre></div>")
		}
	}

	flags := cmd.NonInheritedFlags()
	flags.SetOutput(g.buffer)

	parentFlags := cmd.InheritedFlags()
	parentFlags.SetOutput(g.buffer)

	if flags.HasFlags() || parentFlags.HasFlags() {
		f := make(map[string]*pflag.Flag)
		addFlags(f, flags)
		addFlags(f, parentFlags)

		if len(f) > 0 {
			names := make([]string, len(f))
			i := 0
			for n := range f {
				names[i] = n
				i++
			}
			sort.Strings(names)

			genShorthand := false
			for _, v := range f {
				if v.Shorthand != "" && v.ShorthandDeprecated == "" {
					genShorthand = true
					break
				}
			}

			g.emit("<table class=\"command-flags\">")
			g.emit("<thead>")
			g.emit("<tr>")
			g.emit("<th>Flags</th>")
			if genShorthand {
				g.emit("<th>Shorthand</th>")
			}
			g.emit("<th>Description</th>")
			g.emit("</tr>")
			g.emit("</thead>")
			g.emit("<tbody>")

			for _, n := range names {
				g.genFlag(f[n], genShorthand)
			}

			g.emit("</tbody>")
			g.emit("</table>")
		}
	}

	if len(cmd.Example) > 0 {
		g.emit("<h3 id=\"", normalizeID(cmd.CommandPath()), " Examples\">", "Examples", "</h3>")
		g.emit("<pre class=\"language-bash\"><code>", html.EscapeString(cmd.Example))
		g.emit("</code></pre>")
	}
}

func addFlags(f map[string]*pflag.Flag, s *pflag.FlagSet) {
	s.VisitAll(func(flag *pflag.Flag) {
		if flag.Deprecated != "" || flag.Hidden {
			return
		}

		if flag.Name == help {
			return
		}

		f[flag.Name] = flag
	})
}

func (g *generator) genFlag(flag *pflag.Flag, genShorthand bool) {
	varname, usage := unquoteUsage(flag)
	if varname != "" {
		varname = " <" + varname + ">"
	}

	def := ""
	if flag.Value.Type() == "string" {
		def = fmt.Sprintf(" (default `%s`)", flag.DefValue)
	} else if flag.Value.Type() != "bool" {
		def = fmt.Sprintf(" (default `%s`)", flag.DefValue)
	}

	g.emit("<tr>")
	g.emit("<td><code>", "--", flag.Name, html.EscapeString(varname), "</code></td>")

	if genShorthand {
		if flag.Shorthand != "" && flag.ShorthandDeprecated == "" {
			g.emit("<td><code>", "-", flag.Shorthand, "</code></td>")
		} else {
			g.emit("<td></td>")
		}
	}

	g.emit("<td>", html.EscapeString(usage), " ", def, "</td>")
	g.emit("</tr>")
}

func (g *generator) emitText(text string) {
	paras := strings.Split(text, "\n\n")
	for _, p := range paras {
		g.emit("<p>", html.EscapeString(p), "</p>")
	}
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

func normalizeID(id string) string {
	id = strings.Replace(id, " ", "-", -1)
	return strings.Replace(id, ".", "-", -1)
}

func (g *generator) genVars(root *cobra.Command) {
	envVars := env.VarDescriptions()

	count := 0
	for _, v := range envVars {
		if v.Hidden {
			continue
		}
		count++
	}

	if count == 0 {
		return
	}

	g.emit("<h2 id=\"envvars\">Environment variables</h2>")

	g.emit("These environment variables affect the behavior of the <code>", root.Name(), "</code> command.")

	g.emit("<table class=\"envvars\">")
	g.emit("<thead>")
	g.emit("<tr>")
	g.emit("<th>Variable Name</th>")
	g.emit("<th>Type</th>")
	g.emit("<th>Default Value</th>")
	g.emit("<th>Description</th>")
	g.emit("</tr>")
	g.emit("</thead>")
	g.emit("<tbody>")

	for _, v := range envVars {
		if v.Hidden {
			continue
		}

		if v.Deprecated {
			g.emit("<tr class='deprecated'>")
		} else {
			g.emit("<tr>")
		}
		g.emit("<td><code>", html.EscapeString(v.Name), "</code></td>")

		switch v.Type {
		case env.STRING:
			g.emit("<td>String</td>")
		case env.BOOL:
			g.emit("<td>Boolean</td>")
		case env.INT:
			g.emit("<td>Integer</td>")
		case env.FLOAT:
			g.emit("<td>Floating-Point</td>")
		case env.DURATION:
			g.emit("<td>Time Duration</td>")
		}

		g.emit("<td><code>", html.EscapeString(v.DefaultValue), "</code></td>")
		g.emit("<td>", html.EscapeString(v.Description), "</td>")
		g.emit("</tr>")
	}

	g.emit("</tbody>")
	g.emit("</table>")
}

func (g *generator) genAnnotations(root *cobra.Command) {
	anns := annotations.Descriptions()

	count := 0
	for _, a := range anns {
		if a.Hidden {
			continue
		}
		count++
	}

	if count == 0 {
		return
	}

	g.emit("<h2 id=\"annotations\">Annotations</h2>")

	g.emit("These resource annotations are used by the <code>", root.Name(), "</code> command.")

	g.emit("<table class=\"annotations\">")
	g.emit("<thead>")
	g.emit("<tr>")
	g.emit("<th>Annotation Name</th>")
	g.emit("<th>Description</th>")
	g.emit("</tr>")
	g.emit("</thead>")
	g.emit("<tbody>")

	for _, a := range anns {
		if a.Hidden {
			continue
		}

		if a.Deprecated {
			g.emit("<tr class='deprecated'>")
		} else {
			g.emit("<tr>")
		}
		g.emit("<td><code>", html.EscapeString(a.Name), "</code></td>")

		g.emit("<td>", html.EscapeString(a.Description), "</td>")
		g.emit("</tr>")
	}

	g.emit("</tbody>")
	g.emit("</table>")
}
