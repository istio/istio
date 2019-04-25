// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testscript

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/errgo.v2/fmt/errors"
)

// mergeCoverProfile merges the coverage information in f into
// cover. It assumes that the coverage information in f is
// always produced from the same binary for every call.
func mergeCoverProfile(cover *testing.Cover, r io.Reader) error {
	scanner, err := newProfileScanner(r)
	if err != nil {
		return errors.Wrap(err)
	}
	if scanner.Mode() != testing.CoverMode() {
		return errors.Newf("unexpected coverage mode in subcommand")
	}
	if cover.Mode == "" {
		cover.Mode = scanner.Mode()
	}
	isCount := cover.Mode == "count"
	if cover.Counters == nil {
		cover.Counters = make(map[string][]uint32)
		cover.Blocks = make(map[string][]testing.CoverBlock)
	}

	// Note that we rely on the fact that the coverage is written
	// out file-by-file, with all blocks for a file in sequence.
	var (
		filename string
		blockId  uint32
		counters []uint32
		blocks   []testing.CoverBlock
	)
	flush := func() {
		if len(counters) > 0 {
			cover.Counters[filename] = counters
			cover.Blocks[filename] = blocks
		}
	}
	for scanner.Scan() {
		block := scanner.Block()
		if scanner.Filename() != filename {
			flush()
			filename = scanner.Filename()
			counters = cover.Counters[filename]
			blocks = cover.Blocks[filename]
			blockId = 0
		} else {
			blockId++
		}
		if int(blockId) >= len(counters) {
			counters = append(counters, block.Count)
			blocks = append(blocks, block.CoverBlock)
			continue
		}
		// TODO check that block.CoverBlock == blocks[blockId] ?
		if isCount {
			counters[blockId] += block.Count
		} else {
			counters[blockId] |= block.Count
		}
	}
	flush()
	if scanner.Err() != nil {
		return errors.Notef(err, nil, "error scanning profile")
	}
	return nil
}

var (
	coverChan chan *os.File
	coverDone chan testing.Cover
)

func goCoverProfileMerge() {
	if coverChan != nil {
		panic("RunMain called twice!")
	}
	coverChan = make(chan *os.File)
	coverDone = make(chan testing.Cover)
	go mergeCoverProfiles()
}

func mergeCoverProfiles() {
	var cover testing.Cover
	for f := range coverChan {
		if err := mergeCoverProfile(&cover, f); err != nil {
			log.Printf("cannot merge coverage profile from %v: %v", f.Name(), err)
		}
		f.Close()
		os.Remove(f.Name())
	}
	coverDone <- cover
}

func finalizeCoverProfile() error {
	cprof := coverProfile()
	if cprof == "" {
		return nil
	}
	f, err := os.Open(cprof)
	if err != nil {
		return errors.Notef(err, nil, "cannot open existing cover profile")
	}
	coverChan <- f
	close(coverChan)
	cover := <-coverDone
	f, err = os.Create(cprof)
	if err != nil {
		return errors.Notef(err, nil, "cannot create cover profile")
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if err := writeCoverProfile1(w, cover); err != nil {
		return errors.Wrap(err)
	}
	if err := w.Flush(); err != nil {
		return errors.Wrap(err)
	}
	if err := f.Close(); err != nil {
		return errors.Wrap(err)
	}
	return nil
}

func writeCoverProfile1(w io.Writer, cover testing.Cover) error {
	fmt.Fprintf(w, "mode: %s\n", cover.Mode)
	var active, total int64
	var count uint32
	for name, counts := range cover.Counters {
		blocks := cover.Blocks[name]
		for i := range counts {
			stmts := int64(blocks[i].Stmts)
			total += stmts
			count = atomic.LoadUint32(&counts[i]) // For -mode=atomic.
			if count > 0 {
				active += stmts
			}
			_, err := fmt.Fprintf(w, "%s:%d.%d,%d.%d %d %d\n", name,
				blocks[i].Line0, blocks[i].Col0,
				blocks[i].Line1, blocks[i].Col1,
				stmts,
				count,
			)
			if err != nil {
				return errors.Wrap(err)
			}
		}
	}
	if total == 0 {
		total = 1
	}
	fmt.Printf("total coverage: %.1f%% of statements%s\n", 100*float64(active)/float64(total), cover.CoveredPackages)
	return nil
}

type profileScanner struct {
	mode     string
	err      error
	scanner  *bufio.Scanner
	filename string
	block    coverBlock
}

type coverBlock struct {
	testing.CoverBlock
	Count uint32
}

var profileLineRe = regexp.MustCompile(`^(.+):([0-9]+)\.([0-9]+),([0-9]+)\.([0-9]+) ([0-9]+) ([0-9]+)$`)

func toInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return i
}

func newProfileScanner(r io.Reader) (*profileScanner, error) {
	s := &profileScanner{
		scanner: bufio.NewScanner(r),
	}
	// First line is "mode: foo", where foo is "set", "count", or "atomic".
	// Rest of file is in the format
	//	encoding/base64/base64.go:34.44,37.40 3 1
	// where the fields are: name.go:line.column,line.column numberOfStatements count
	if !s.scanner.Scan() {
		return nil, errors.Newf("no lines found in profile: %v", s.Err())
	}
	line := s.scanner.Text()
	mode := strings.TrimPrefix(line, "mode: ")
	if len(mode) == len(line) {
		return nil, fmt.Errorf("bad mode line %q", line)
	}
	s.mode = mode
	return s, nil
}

// Mode returns the profile's coverage mode (one of "atomic", "count:
// or "set").
func (s *profileScanner) Mode() string {
	return s.mode
}

// Err returns any error encountered when scanning a profile.
func (s *profileScanner) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Block returns the most recently scanned profile block, or the zero
// block if Scan has not been called or has returned false.
func (s *profileScanner) Block() coverBlock {
	if s.err == nil {
		return s.block
	}
	return coverBlock{}
}

// Filename returns the filename of the most recently scanned profile
// block, or the empty string if Scan has not been called or has
// returned false.
func (s *profileScanner) Filename() string {
	if s.err == nil {
		return s.filename
	}
	return ""
}

// Scan scans the next line in a coverage profile and reports whether
// a line was found.
func (s *profileScanner) Scan() bool {
	if s.err != nil {
		return false
	}
	if !s.scanner.Scan() {
		s.err = io.EOF
		return false
	}
	m := profileLineRe.FindStringSubmatch(s.scanner.Text())
	if m == nil {
		s.err = errors.Newf("line %q doesn't match expected format %v", m, profileLineRe)
		return false
	}
	s.filename = m[1]
	s.block = coverBlock{
		CoverBlock: testing.CoverBlock{
			Line0: uint32(toInt(m[2])),
			Col0:  uint16(toInt(m[3])),
			Line1: uint32(toInt(m[4])),
			Col1:  uint16(toInt(m[5])),
			Stmts: uint16(toInt(m[6])),
		},
		Count: uint32(toInt(m[7])),
	}
	return true
}
