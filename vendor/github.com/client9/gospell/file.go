package gospell

import (
	"github.com/client9/gospell/plaintext"

	"strings"
)

// Diff represent a unknown word in a file
type Diff struct {
	Filename string
	Path     string
	Original string
	Line     string
	LineNum  int
}

// SpellFile is attempts to spell-check a file.  This interface is not
// very good so expect changes.
func SpellFile(gs *GoSpell, ext plaintext.Extractor, raw []byte) []Diff {
	out := []Diff{}

	// remove any golang templates
	raw = plaintext.StripTemplate(raw)

	// extract plain text
	raw = ext.Text(raw)

	// do character conversion "smart quotes" to quotes, etc
	// as specified in the Affix file
	rawstring := gs.InputConversion(raw)

	// zap URLS
	s := RemoveURL(rawstring)
	// zap file paths
	s = RemovePath(s)

	for linenum, line := range strings.Split(s, "\n") {
		// now get words
		words := gs.Split(line)
		for _, word := range words {
			// HACK
			word = strings.Trim(word, "'")
			if known := gs.Spell(word); !known {
				out = append(out, Diff{
					Line:     line,
					LineNum:  linenum + 1,
					Original: word,
				})
			}
		}
	}
	return out
}
