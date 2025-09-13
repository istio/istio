//go:build !agent
// +build !agent

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

/*
NOTICE: The zsh constants are derived from the kubectl completion code
(k8s.io/kubernetes/pkg/kubectl/cmd/completion/completion.go), with the
following copyright/license:

Copyright 2016 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package collateral

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/collateral/metrics"
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

	// EmitZshCompletion controls whether to produce zsh completion files.
	EmitZshCompletion bool

	// EmitMarkdown controls whether to produce markdown documentation files.
	EmitMarkdown bool

	// EmitHTMLFragmentWithFrontMatter controls whether to produce HTML fragments with Jekyll/Hugo front matter.
	EmitHTMLFragmentWithFrontMatter bool

	// ManPageInfo provides extra information necessary when emitting man pages.
	ManPageInfo doc.GenManHeader

	// Predicates to use to filter environment variables and metrics. If not set, all items will be selected.
	Predicates Predicates
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
		if err := genHTMLFragment(root, c.OutputDir+"/"+root.Name()+".html", c.Predicates); err != nil {
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

	if c.EmitZshCompletion {

		// Constants used in zsh completion file
		zshInitialization := `#compdef ` + root.Name() + `

__istio_bash_source() {
	alias shopt=':'
	alias _expand=_bash_expand
	alias _complete=_bash_comp
	emulate -L sh
	setopt kshglob noshglob braceexpand
	source "$@"
}
__istio_type() {
	# -t is not supported by zsh
	if [ "$1" == "-t" ]; then
		shift
		# fake Bash 4 to disable "complete -o nospace". Instead
		# "compopt +-o nospace" is used in the code to toggle trailing
		# spaces. We don't support that, but leave trailing spaces on
		# all the time
		if [ "$1" = "__istio_compopt" ]; then
			echo builtin
			return 0
		fi
	fi
	type "$@"
}
__istio_compgen() {
	local completions w
	completions=( $(compgen "$@") ) || return $?
	# filter by given word as prefix
	while [[ "$1" = -* && "$1" != -- ]]; do
		shift
		shift
	done
	if [[ "$1" == -- ]]; then
		shift
	fi
	for w in "${completions[@]}"; do
		if [[ "${w}" = "$1"* ]]; then
			echo "${w}"
		fi
	done
}
__istio_compopt() {
	true # don't do anything. Not supported by bashcompinit in zsh
}
__istio_ltrim_colon_completions()
{
	if [[ "$1" == *:* && "$COMP_WORDBREAKS" == *:* ]]; then
		# Remove colon-word prefix from COMPREPLY items
		local colon_word=${1%${1##*:}}
		local i=${#COMPREPLY[*]}
		while [[ $((--i)) -ge 0 ]]; do
			COMPREPLY[$i]=${COMPREPLY[$i]#"$colon_word"}
		done
	fi
}
__istio_get_comp_words_by_ref() {
	cur="${COMP_WORDS[COMP_CWORD]}"
	prev="${COMP_WORDS[${COMP_CWORD}-1]}"
	words=("${COMP_WORDS[@]}")
	cword=("${COMP_CWORD[@]}")
}
__istio_filedir() {
	local RET OLD_IFS w qw
	__istio_debug "_filedir $@ cur=$cur"
	if [[ "$1" = \~* ]]; then
		# somehow does not work. Maybe, zsh does not call this at all
		eval echo "$1"
		return 0
	fi
	OLD_IFS="$IFS"
	IFS=$'\n'
	if [ "$1" = "-d" ]; then
		shift
		RET=( $(compgen -d) )
	else
		RET=( $(compgen -f) )
	fi
	IFS="$OLD_IFS"
	IFS="," __istio_debug "RET=${RET[@]} len=${#RET[@]}"
	for w in ${RET[@]}; do
		if [[ ! "${w}" = "${cur}"* ]]; then
			continue
		fi
		if eval "[[ \"\${w}\" = *.$1 || -d \"\${w}\" ]]"; then
			qw="$(__istio_quote "${w}")"
			if [ -d "${w}" ]; then
				COMPREPLY+=("${qw}/")
			else
				COMPREPLY+=("${qw}")
			fi
		fi
	done
}
__istio_quote() {
	if [[ $1 == \'* || $1 == \"* ]]; then
		# Leave out first character
		printf %q "${1:1}"
	else
		printf %q "$1"
	fi
}
autoload -U +X bashcompinit && bashcompinit
# use word boundary patterns for BSD or GNU sed
LWORD='[[:<:]]'
RWORD='[[:>:]]'
if sed --help 2>&1 | grep -q GNU; then
	LWORD='\<'
	RWORD='\>'
fi
__istio_convert_bash_to_zsh() {
	sed \
	-e 's/declare -F/whence -w/' \
	-e 's/_get_comp_words_by_ref "\$@"/_get_comp_words_by_ref "\$*"/' \
	-e 's/local \([a-zA-Z0-9_]*\)=/local \1; \1=/' \
	-e 's/flags+=("\(--.*\)=")/flags+=("\1"); two_word_flags+=("\1")/' \
	-e 's/must_have_one_flag+=("\(--.*\)=")/must_have_one_flag+=("\1")/' \
	-e "s/${LWORD}_filedir${RWORD}/__istio_filedir/g" \
	-e "s/${LWORD}_get_comp_words_by_ref${RWORD}/__istio_get_comp_words_by_ref/g" \
	-e "s/${LWORD}__ltrim_colon_completions${RWORD}/__istio_ltrim_colon_completions/g" \
	-e "s/${LWORD}compgen${RWORD}/__istio_compgen/g" \
	-e "s/${LWORD}compopt${RWORD}/__istio_compopt/g" \
	-e "s/${LWORD}declare${RWORD}/builtin declare/g" \
	-e "s/\\\$(type${RWORD}/\$(__istio_type/g" \
	<<'BASH_COMPLETION_EOF'
`

		zshTail := `
BASH_COMPLETION_EOF
}

__istio_bash_source <(__istio_convert_bash_to_zsh)
_complete istio 2>/dev/null
`

		// Create the output file.
		outFile, err := os.Create(c.OutputDir + "/_" + root.Name())
		if err != nil {
			return fmt.Errorf("unable to create zsh completion file: %v", err)
		}
		defer func() { _ = outFile.Close() }()

		// Concatenate the head, initialization, generated bash, and tail to the file
		if _, err = outFile.WriteString(zshInitialization); err != nil {
			return fmt.Errorf("unable to output zsh initialization: %v", err)
		}
		if err = root.GenBashCompletion(outFile); err != nil {
			return fmt.Errorf("unable to output zsh completion file: %v", err)
		}
		if _, err = outFile.WriteString(zshTail); err != nil {
			return fmt.Errorf("unable to output zsh tail: %v", err)
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

func genHTMLFragment(cmd *cobra.Command, path string, p Predicates) error {
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
		_ = g.genConfigFile(commands[n].Flags())
	}

	g.genVars(cmd, p.SelectEnv)
	g.genMetrics(p.SelectMetric)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = g.buffer.WriteTo(f)
	_ = f.Close()

	return err
}

func (g *generator) genConfigFile(flags *pflag.FlagSet) error {
	// find all flag names and aliases which contain dots, as these allow
	// structured config files, which aren't intuitive
	deepkeys := make(map[string]string)
	flags.VisitAll(func(f *pflag.Flag) {
		if strings.Contains(f.Name, ".") {
			deepkeys[f.Name] = "--" + f.Name
		}
	})

	if len(deepkeys) < 1 {
		return nil
	}

	deepConfig := buildNestedMap(deepkeys)
	strb, err := yaml.Marshal(deepConfig)
	if err != nil {
		return err
	}
	str := string(strb)

	g.emit("<p/>Accepts deep config files, like:")
	g.emit("<pre class=\"language-yaml\"><code>", str)
	g.emit("</code></pre>")
	return nil
}

func buildNestedMap(flatMap map[string]string) (result map[string]any) {
	result = make(map[string]any)
	for complexkey, value := range flatMap {
		buildMapRecursive(strings.Split(complexkey, "."), result, value)
	}
	return
}

func buildMapRecursive(remainingPath []string, currentPointer map[string]any, value string) {
	if len(remainingPath) == 1 {
		currentPointer[remainingPath[0]] = value
		return
	}
	var nextPointer any
	var existingPath bool
	if nextPointer, existingPath = currentPointer[remainingPath[0]]; !existingPath {
		nextPointer = make(map[string]any)
		currentPointer[remainingPath[0]] = nextPointer
	}
	buildMapRecursive(remainingPath[1:], nextPointer.(map[string]any), value)
}

func (g *generator) genFrontMatter(root *cobra.Command, numEntries int) {
	g.emit("---")
	g.emit("title: ", root.Name())
	g.emit("description: ", root.Short)
	g.emit("generator: pkg-collateral-docs")
	g.emit("number_of_entries: ", strconv.Itoa(numEntries))
	g.emit("max_toc_level: 2")
	g.emit("remove_toc_prefix: '" + root.Name() + " '")
	g.emit("---")
}

type CustomTextFunc func(cmd string) string

var CustomTextForCmds = map[string]CustomTextFunc{
	"completion-bash": func(cmd string) string {
		return fmt.Sprintf(`<p>Generate the autocompletion script for the bash shell.</p>
<p>This script depends on the &#39;bash-completion&#39; package.
If it is not installed already, you can install it via your OS&#39;s package manager.</p>
<p>To load completions in your current shell session:</p>
<pre class="language-bash"><code>source &lt;(%s completion bash)</code></pre>
<p>To load completions for every new session, execute once:</p>
<h4>Linux:</h4>
<pre class="language-bash"><code>%s completion bash &gt; /etc/bash_completion.d/%s</code></pre>
<h4>macOS:</h4>
<pre class="language-bash"><code>%s completion bash &gt; /usr/local/etc/bash_completion.d/%s</code></pre>
<p>You will need to start a new shell for this setup to take effect.</p>`, cmd, cmd, cmd, cmd, cmd)
	},

	"completion-fish": func(cmd string) string {
		return fmt.Sprintf(`<p>Generate the autocompletion script for the fish shell.</p>
<p>To load completions in your current shell session:</p>
<pre class="language-bash"><code>%s completion fish | source</code></pre>
<p>To load completions for every new session, execute once:</p>
<pre class="language-bash"><code>%s completion bash &gt; ~/.config/fish/completions/%s.fish</code></pre>
<p>You will need to start a new shell for this setup to take effect.</p>`, cmd, cmd, cmd)
	},

	"completion-powershell": func(cmd string) string {
		return fmt.Sprintf(`<p>Generate the autocompletion script for PowerShell.</p>
<p>To load completions in your current shell session:</p>
<pre class="language-bash"><code>%s completion powershell | Out-String | Invoke-Expression</code></pre>
<p>To load completions for every new session, add the output of the above command to your powershell profile.</p>`, cmd)
	},

	"completion-zsh": func(cmd string) string {
		return fmt.Sprintf(`<p>Generate the autocompletion script for the zsh shell.</p>
	<p>If shell completion is not already enabled in your environment you will need to enable it. You can execute the following once:</p>
	<pre class="language-bash"><code>echo &#34;autoload -U compinit; compinit&#34; &gt;&gt; ~/.zshrc</code></pre>
	<p>To load completions in your current shell session:</p>
	<pre class="language-bash"><code>source &lt;(%s completion zsh)</code></pre>
	<p>To load completions for every new session, execute once:</p>
	<h4>Linux:</h4>
	<pre class="language-bash"><code>%s completion zsh &gt; &#34;${fpath[1]}/_%s&#34;</code></pre>
	<h4>macOS:</h4>
	<pre class="language-bash"><code>%s completion zsh &gt; $(brew --prefix)/share/zsh/site-functions/_%s</code></pre>
	<p>You will need to start a new shell for this setup to take effect.</p>`, cmd, cmd, cmd, cmd, cmd)
	},
}

func getCustomText(normalizedCmdPath string) (string, bool) {
	parts := strings.Split(normalizedCmdPath, "-")
	if len(parts) < 2 {
		return "", false
	}

	// extract the completion type, e.g. "completion-fish", "completion-zsh"
	completionType := strings.Join(parts[len(parts)-2:], "-")
	// anything before the completion type is the command name (e.g. istioctl, pilot-agent, ...)
	cmd := strings.Join(parts[:len(parts)-2], "-")

	if textFunc, ok := CustomTextForCmds[completionType]; ok {
		return textFunc(cmd), true
	}
	return "", false
}

func (g *generator) genCommand(cmd *cobra.Command) {
	if cmd.Hidden || cmd.Deprecated != "" {
		return
	}

	normalizedCmdPath := normalizeID(cmd.CommandPath())
	if cmd.HasParent() {
		g.emit("<h3 id=\"", normalizedCmdPath, "\">", cmd.CommandPath(), "</h3>")
	}

	// Check whether there is a custom text for this command
	// For now, this only applies to completion commands (bash, zsh, etc.)
	// as the descriptions for these were coming from non-Istio owned package (cobra).
	if customText, ok := getCustomText(normalizedCmdPath); ok {
		g.emit(customText)
	} else {
		if cmd.Long != "" {
			g.emitText(cmd.Long)
		} else if cmd.Short != "" {
			g.emitText(cmd.Short)
		}
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
		g.emit("<h4 id=\"", normalizeID(cmd.CommandPath()), " Examples\">", "Examples", "</h4>")
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

func (g *generator) genVars(root *cobra.Command, selectFn SelectEnvFn) {
	if selectFn == nil {
		selectFn = DefaultSelectEnvFn
	}

	envVars := env.VarDescriptions()

	count := 0
	for _, v := range envVars {
		if v.Hidden {
			continue
		}
		if !selectFn(v) {
			continue
		}
		count++
	}

	if count == 0 {
		return
	}

	g.emit("<h2 id=\"envvars\">Environment variables</h2>")

	g.emit("These environment variables affect the behavior of the <code>", root.Name(), "</code> command. ")

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
		if !selectFn(v) {
			continue
		}

		id := slugifyID(html.EscapeString(v.Name))
		if v.Deprecated {
			g.emit(fmt.Sprintf("<tr id='%s' class='deprecated'>", id))
		} else {
			g.emit(fmt.Sprintf("<tr id='%s'>", id))
		}
		g.emit(fmt.Sprintf("<td><a href='#%s'><code>%s</code></a></td>", id, html.EscapeString(v.Name)))

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
		case env.OTHER:
			g.emit(fmt.Sprintf("<td>%s</td>", v.GoType))
		}

		g.emit("<td><code>", html.EscapeString(v.DefaultValue), "</code></td>")
		g.emit("<td>", html.EscapeString(v.Description), "</td>")
		g.emit("</tr>")
	}

	g.emit("</tbody>")
	g.emit("</table>")
}

func (g *generator) genMetrics(selectFn SelectMetricFn) {
	if selectFn == nil {
		selectFn = DefaultSelectMetricFn
	}

	g.emit(`<h2 id="metrics">Exported metrics</h2>
<table class="metrics">
<thead>
<tr><th>Metric Name</th><th>Type</th><th>Description</th></tr>
</thead>
<tbody>`)

	for _, metric := range metrics.ExportedMetrics() {
		if !selectFn(metric) {
			continue
		}
		g.emit("<tr><td><code>", metric.Name, "</code></td><td><code>", metric.Type, "</code></td><td>", metric.Description, "</td></tr>")
	}

	g.emit(`</tbody>
</table>`)
}

func slugifyID(text string) string {
	text = strings.ToLower(text)

	// Replace non-alphanumeric characters with dashes
	re := regexp.MustCompile(`[^a-z0-9]+`)
	text = re.ReplaceAllString(text, "-")

	return strings.Trim(text, "-")
}
