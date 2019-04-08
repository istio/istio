package gospell

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// AffixType is either an affix prefix or suffix
type AffixType int

// specific Affix types
const (
	Prefix AffixType = iota
	Suffix
)

// Affix is a rule for affix (adding prefixes or suffixes)
type Affix struct {
	Type         AffixType // either PFX or SFX
	CrossProduct bool
	Rules        []Rule
}

// Expand provides all variations of a given word based on this affix rule
func (a Affix) Expand(word string, out []string) []string {
	for _, r := range a.Rules {
		if r.matcher != nil && !r.matcher.MatchString(word) {
			continue
		}
		if a.Type == Prefix {
			out = append(out, r.AffixText+word)
			// TODO is does Strip apply to prefixes too?
		} else {
			stripWord := word
			if r.Strip != "" && strings.HasSuffix(word, r.Strip) {
				stripWord = word[:len(word)-len(r.Strip)]
			}
			out = append(out, stripWord+r.AffixText)
		}
	}
	return out
}

// Rule is a Affix rule
type Rule struct {
	Strip     string
	AffixText string         // suffix or prefix text to add
	Pattern   string         // original matching pattern from AFF file
	matcher   *regexp.Regexp // matcher to see if this rule applies or not
}

// DictConfig is a partial representation of a Hunspell AFF (Affix) file.
type DictConfig struct {
	Flag              string
	TryChars          string
	WordChars         string
	NoSuggestFlag     rune
	IconvReplacements []string
	Replacements      [][2]string
	AffixMap          map[rune]Affix
	CamelCase         int
	CompoundMin       int
	CompoundOnly      string
	CompoundRule      []string
	compoundMap       map[rune][]string
}

// Expand expands a word/affix using dictionary/affix rules
//  This also supports CompoundRule flags
func (a DictConfig) Expand(wordAffix string, out []string) ([]string, error) {
	out = out[:0]
	idx := strings.Index(wordAffix, "/")

	// not found
	if idx == -1 {
		out = append(out, wordAffix)
		return out, nil
	}
	if idx == 0 || idx+1 == len(wordAffix) {
		return nil, fmt.Errorf("Slash char found in first or last position")
	}
	// safe
	word, keyString := wordAffix[:idx], wordAffix[idx+1:]

	// check to see if any of the flags are in the
	// "compound only".  If so then nothing to add
	compoundOnly := false
	for _, key := range keyString {
		if strings.IndexRune(a.CompoundOnly, key) != -1 {
			compoundOnly = true
			continue
		}
		if _, ok := a.compoundMap[key]; !ok {
			// the isn't a compound flag
			continue
		}
		// is a compound flag
		a.compoundMap[key] = append(a.compoundMap[key], word)
	}

	if compoundOnly {
		return out, nil
	}

	out = append(out, word)
	prefixes := make([]Affix, 0, 5)
	suffixes := make([]Affix, 0, 5)
	for _, key := range keyString {
		// want keyString to []?something?
		// then iterate over that
		af, ok := a.AffixMap[key]
		if !ok {
			// is it compound flag?
			if _, ok := a.compoundMap[key]; ok {
				continue
			}
			// is it a NoSuggest?
			if key == a.NoSuggestFlag {
				continue
			}
			// no idea
			return nil, fmt.Errorf("unable to find affix key %v", key)
		}
		if !af.CrossProduct {
			out = af.Expand(word, out)
			continue
		}
		if af.Type == Prefix {
			prefixes = append(prefixes, af)
		} else {
			suffixes = append(suffixes, af)
		}
	}

	// expand all suffixes with out any prefixes
	for _, suf := range suffixes {
		out = suf.Expand(word, out)
	}
	for _, pre := range prefixes {
		prewords := pre.Expand(word, nil)
		out = append(out, prewords...)

		// now do cross product
		for _, suf := range suffixes {
			for _, w := range prewords {
				out = suf.Expand(w, out)
			}
		}
	}
	return out, nil
}

func isCrossProduct(val string) (bool, error) {
	switch val {
	case "Y":
		return true, nil
	case "N":
		return false, nil
	}
	return false, fmt.Errorf("CrossProduct is not Y or N: got %q", val)
}

// NewDictConfig reads an Hunspell AFF file
func NewDictConfig(file io.Reader) (*DictConfig, error) {
	aff := DictConfig{
		Flag:        "ASCII",
		AffixMap:    make(map[rune]Affix),
		compoundMap: make(map[rune][]string),
		CompoundMin: 3, // default in Hunspell
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "#":
			continue
		case "TRY":
			if len(parts) != 2 {
				return nil, fmt.Errorf("TRY stanza had %d fields, expected 2", len(parts))
			}
			aff.TryChars = parts[1]
		case "ICONV":
			// if only 2 fields, then its the first stanza that just provides a count
			//  we don't care, as we dynamically allocate
			if len(parts) == 2 {
				continue
			}
			if len(parts) != 3 {
				return nil, fmt.Errorf("ICONV stanza had %d fields, expected 2", len(parts))
			}
			// we have 3
			aff.IconvReplacements = append(aff.IconvReplacements, parts[1], parts[2])
		case "REP":
			// if only 2 fields, then its the first stanza that just provides a count
			//  we don't care, as we dynamically allocate
			if len(parts) == 2 {
				continue
			}
			if len(parts) != 3 {
				return nil, fmt.Errorf("REP stanza had %d fields, expected 2", len(parts))
			}
			// we have 3
			aff.Replacements = append(aff.Replacements, [2]string{parts[1], parts[2]})
		case "COMPOUNDMIN":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %q expected number", parts[1])
			}
			aff.CompoundMin = int(val)
		case "ONLYINCOMPOUND":
			if len(parts) != 2 {
				return nil, fmt.Errorf("ONLYINCOMPOUND stanza had %d fields, expected 2", len(parts))
			}
			aff.CompoundOnly = parts[1]
		case "COMPOUNDRULE":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDRULE stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				aff.CompoundRule = make([]string, 0, val)
			} else {
				aff.CompoundRule = append(aff.CompoundRule, parts[1])
				for _, char := range parts[1] {
					if _, ok := aff.compoundMap[char]; !ok {
						aff.compoundMap[char] = []string{}
					}
				}
			}
		case "NOSUGGEST":
			if len(parts) != 2 {
				return nil, fmt.Errorf("NOSUGGEST stanza had %d fields, expected 2", len(parts))
			}
			// should use runes or parse correctly
			chars := []rune(parts[1])
			if len(chars) != 1 {
				return nil, fmt.Errorf("NOSUGGEST stanza had more than one flag: %q", parts[1])
			}
			aff.NoSuggestFlag = chars[0]
		case "WORDCHARS":
			if len(parts) != 2 {
				return nil, fmt.Errorf("WORDCHAR stanza had %d fields, expected 2", len(parts))
			}
			aff.WordChars = parts[1]
		case "FLAG":
			if len(parts) != 2 {
				return nil, fmt.Errorf("FLAG stanza had %d, expected 1", len(parts))
			}
			aff.Flag = parts[1]
			return nil, fmt.Errorf("FLAG stanza not yet supported")
		case "PFX", "SFX":
			atype := Prefix
			if parts[0] == "SFX" {
				atype = Suffix
			}

			switch len(parts) {
			case 4:
				cross, err := isCrossProduct(parts[2])
				if err != nil {
					return nil, err
				}
				// this is a new Affix!
				a := Affix{
					Type:         atype,
					CrossProduct: cross,
				}
				flag := rune(parts[1][0])
				aff.AffixMap[flag] = a
			case 5:
				// does this need to be split out into suffix and prefix?
				flag := rune(parts[1][0])
				a, ok := aff.AffixMap[flag]
				if !ok {
					return nil, fmt.Errorf("Got rules for flag %q but no definition", flag)
				}

				strip := ""
				if parts[2] != "0" {
					strip = parts[2]
				}

				var matcher *regexp.Regexp
				var err error
				pat := parts[4]
				if pat != "." {
					if a.Type == Prefix {
						pat = "^" + pat
					} else {
						pat = pat + "$"
					}
					matcher, err = regexp.Compile(pat)
					if err != nil {
						return nil, fmt.Errorf("Unable to compile %s", pat)
					}
				}

				a.Rules = append(a.Rules, Rule{
					Strip:     strip,
					AffixText: parts[3],
					Pattern:   parts[4],
					matcher:   matcher,
				})
				aff.AffixMap[flag] = a
			default:
				return nil, fmt.Errorf("%s stanza had %d fields, expected 4 or 5", parts[0], len(parts))
			}
		default:
			// nothing
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &aff, nil
}
