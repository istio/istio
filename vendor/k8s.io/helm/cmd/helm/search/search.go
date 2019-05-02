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

/*Package search provides client-side repository searching.

This supports building an in-memory search index based on the contents of
multiple repositories, and then using string matching or regular expressions
to find matches.
*/
package search

import (
	"errors"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver"
	"k8s.io/helm/pkg/repo"
)

// Result is a search result.
//
// Score indicates how close it is to match. The higher the score, the longer
// the distance.
type Result struct {
	Name  string
	Score int
	Chart *repo.ChartVersion
}

// Index is a searchable index of chart information.
type Index struct {
	lines  map[string]string
	charts map[string]*repo.ChartVersion
}

const sep = "\v"

// NewIndex creats a new Index.
func NewIndex() *Index {
	return &Index{lines: map[string]string{}, charts: map[string]*repo.ChartVersion{}}
}

// verSep is a separator for version fields in map keys.
const verSep = "$$"

// AddRepo adds a repository index to the search index.
func (i *Index) AddRepo(rname string, ind *repo.IndexFile, all bool) {
	ind.SortEntries()
	for name, ref := range ind.Entries {
		if len(ref) == 0 {
			// Skip chart names that have zero releases.
			continue
		}
		// By convention, an index file is supposed to have the newest at the
		// 0 slot, so our best bet is to grab the 0 entry and build the index
		// entry off of that.
		// Note: Do not use filePath.Join since on Windows it will return \
		//       which results in a repo name that cannot be understood.
		fname := path.Join(rname, name)
		if !all {
			i.lines[fname] = indstr(rname, ref[0])
			i.charts[fname] = ref[0]
			continue
		}

		// If 'all' is set, then we go through all of the refs, and add them all
		// to the index. This will generate a lot of near-duplicate entries.
		for _, rr := range ref {
			versionedName := fname + verSep + rr.Version
			i.lines[versionedName] = indstr(rname, rr)
			i.charts[versionedName] = rr
		}
	}
}

// All returns all charts in the index as if they were search results.
//
// Each will be given a score of 0.
func (i *Index) All() []*Result {
	res := make([]*Result, len(i.charts))
	j := 0
	for name, ch := range i.charts {
		parts := strings.Split(name, verSep)
		res[j] = &Result{
			Name:  parts[0],
			Chart: ch,
		}
		j++
	}
	return res
}

// Search searches an index for the given term.
//
// Threshold indicates the maximum score a term may have before being marked
// irrelevant. (Low score means higher relevance. Golf, not bowling.)
//
// If regexp is true, the term is treated as a regular expression. Otherwise,
// term is treated as a literal string.
func (i *Index) Search(term string, threshold int, regexp bool) ([]*Result, error) {
	if regexp {
		return i.SearchRegexp(term, threshold)
	}
	return i.SearchLiteral(term, threshold), nil
}

// calcScore calculates a score for a match.
func (i *Index) calcScore(index int, matchline string) int {

	// This is currently tied to the fact that sep is a single char.
	splits := []int{}
	s := rune(sep[0])
	for i, ch := range matchline {
		if ch == s {
			splits = append(splits, i)
		}
	}

	for i, pos := range splits {
		if index > pos {
			continue
		}
		return i
	}
	return len(splits)
}

// SearchLiteral does a literal string search (no regexp).
func (i *Index) SearchLiteral(term string, threshold int) []*Result {
	term = strings.ToLower(term)
	buf := []*Result{}
	for k, v := range i.lines {
		lk := strings.ToLower(k)
		lv := strings.ToLower(v)
		res := strings.Index(lv, term)
		if score := i.calcScore(res, lv); res != -1 && score < threshold {
			parts := strings.Split(lk, verSep) // Remove version, if it is there.
			buf = append(buf, &Result{Name: parts[0], Score: score, Chart: i.charts[k]})
		}
	}
	return buf
}

// SearchRegexp searches using a regular expression.
func (i *Index) SearchRegexp(re string, threshold int) ([]*Result, error) {
	matcher, err := regexp.Compile(re)
	if err != nil {
		return []*Result{}, err
	}
	buf := []*Result{}
	for k, v := range i.lines {
		ind := matcher.FindStringIndex(v)
		if len(ind) == 0 {
			continue
		}
		if score := i.calcScore(ind[0], v); ind[0] >= 0 && score < threshold {
			parts := strings.Split(k, verSep) // Remove version, if it is there.
			buf = append(buf, &Result{Name: parts[0], Score: score, Chart: i.charts[k]})
		}
	}
	return buf, nil
}

// Chart returns the ChartVersion for a particular name.
func (i *Index) Chart(name string) (*repo.ChartVersion, error) {
	c, ok := i.charts[name]
	if !ok {
		return nil, errors.New("no such chart")
	}
	return c, nil
}

// SortScore does an in-place sort of the results.
//
// Lowest scores are highest on the list. Matching scores are subsorted alphabetically.
func SortScore(r []*Result) {
	sort.Sort(scoreSorter(r))
}

// scoreSorter sorts results by score, and subsorts by alpha Name.
type scoreSorter []*Result

// Len returns the length of this scoreSorter.
func (s scoreSorter) Len() int { return len(s) }

// Swap performs an in-place swap.
func (s scoreSorter) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// Less compares a to b, and returns true if a is less than b.
func (s scoreSorter) Less(a, b int) bool {
	first := s[a]
	second := s[b]

	if first.Score > second.Score {
		return false
	}
	if first.Score < second.Score {
		return true
	}
	if first.Name == second.Name {
		v1, err := semver.NewVersion(first.Chart.Version)
		if err != nil {
			return true
		}
		v2, err := semver.NewVersion(second.Chart.Version)
		if err != nil {
			return true
		}
		// Sort so that the newest chart is higher than the oldest chart. This is
		// the opposite of what you'd expect in a function called Less.
		return v1.GreaterThan(v2)
	}
	return first.Name < second.Name
}

func indstr(name string, ref *repo.ChartVersion) string {
	i := ref.Name + sep + name + "/" + ref.Name + sep +
		ref.Description + sep + strings.Join(ref.Keywords, " ")
	return i
}
