package moniker

import (
	"math/rand"
	"strings"
	"time"
)

// New returns a generic namer using the default word lists.
func New() Namer {
	return &defaultNamer{
		Descriptor: Descriptors,
		Noun:       Animals,
		r:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

type defaultNamer struct {
	Descriptor, Noun []string
	r                *rand.Rand
}

func (n *defaultNamer) NameSep(sep string) string {
	a := n.Descriptor[n.r.Intn(len(n.Descriptor))]
	b := n.Noun[n.r.Intn(len(n.Noun))]
	return strings.Join([]string{a, b}, sep)
}

func (n *defaultNamer) Name() string {
	return n.NameSep(" ")
}

// NewAlliterator returns a Namer that alliterates.
//
//	wonky wombat
//	racing rabbit
//	alliterating alligator
//
// FIXME: This isn't working yet.
func NewAlliterator() Namer {
	return &alliterator{
		Descriptor: Descriptors,
		Noun:       Animals,
		r:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

type alliterator struct {
	Descriptor, Noun []string
	r                *rand.Rand
}

func (n *alliterator) Name() string {
	return n.NameSep(" ")
}
func (n *alliterator) NameSep(sep string) string {
	return ""
}

// Namer describes anything capable of generating a name.
type Namer interface {
	// Name returns a generated name.
	Name() string
	// NameSep returns a generated name with words separated by the given string.
	NameSep(string) string
}
