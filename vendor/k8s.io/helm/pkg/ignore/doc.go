/*
Copyright The Helm Authors.

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

/*Package ignore provides tools for writing ignore files (a la .gitignore).

This provides both an ignore parser and a file-aware processor.

The format of ignore files closely follows, but does not exactly match, the
format for .gitignore files (https://git-scm.com/docs/gitignore).

The formatting rules are as follows:

	- Parsing is line-by-line
	- Empty lines are ignored
	- Lines the begin with # (comments) will be ignored
	- Leading and trailing spaces are always ignored
	- Inline comments are NOT supported ('foo* # Any foo' does not contain a comment)
	- There is no support for multi-line patterns
	- Shell glob patterns are supported. See Go's "path/filepath".Match
	- If a pattern begins with a leading !, the match will be negated.
	- If a pattern begins with a leading /, only paths relatively rooted will match.
	- If the pattern ends with a trailing /, only directories will match
	- If a pattern contains no slashes, file basenames are tested (not paths)
	- The pattern sequence "**", while legal in a glob, will cause an error here
	  (to indicate incompatibility with .gitignore).

Example:

	# Match any file named foo.txt
	foo.txt

	# Match any text file
	*.txt

	# Match only directories named mydir
	mydir/

	# Match only text files in the top-level directory
	/*.txt

	# Match only the file foo.txt in the top-level directory
	/foo.txt

	# Match any file named ab.txt, ac.txt, or ad.txt
	a[b-d].txt

Notable differences from .gitignore:
	- The '**' syntax is not supported.
	- The globbing library is Go's 'filepath.Match', not fnmatch(3)
	- Trailing spaces are always ignored (there is no supported escape sequence)
	- The evaluation of escape sequences has not been tested for compatibility
	- There is no support for '\!' as a special leading sequence.
*/
package ignore // import "k8s.io/helm/pkg/ignore"
