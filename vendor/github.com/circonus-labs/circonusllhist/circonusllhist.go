// Copyright 2016, Circonus, Inc. All rights reserved.
// See the LICENSE file.

// Package circllhist provides an implementation of Circonus' fixed log-linear
// histogram data structure.  This allows tracking of histograms in a
// composable way such that accurate error can be reasoned about.
package circonusllhist

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultHistSize = uint16(100)
)

var powerOfTen = [...]float64{
	1, 10, 100, 1000, 10000, 100000, 1e+06, 1e+07, 1e+08, 1e+09, 1e+10,
	1e+11, 1e+12, 1e+13, 1e+14, 1e+15, 1e+16, 1e+17, 1e+18, 1e+19, 1e+20,
	1e+21, 1e+22, 1e+23, 1e+24, 1e+25, 1e+26, 1e+27, 1e+28, 1e+29, 1e+30,
	1e+31, 1e+32, 1e+33, 1e+34, 1e+35, 1e+36, 1e+37, 1e+38, 1e+39, 1e+40,
	1e+41, 1e+42, 1e+43, 1e+44, 1e+45, 1e+46, 1e+47, 1e+48, 1e+49, 1e+50,
	1e+51, 1e+52, 1e+53, 1e+54, 1e+55, 1e+56, 1e+57, 1e+58, 1e+59, 1e+60,
	1e+61, 1e+62, 1e+63, 1e+64, 1e+65, 1e+66, 1e+67, 1e+68, 1e+69, 1e+70,
	1e+71, 1e+72, 1e+73, 1e+74, 1e+75, 1e+76, 1e+77, 1e+78, 1e+79, 1e+80,
	1e+81, 1e+82, 1e+83, 1e+84, 1e+85, 1e+86, 1e+87, 1e+88, 1e+89, 1e+90,
	1e+91, 1e+92, 1e+93, 1e+94, 1e+95, 1e+96, 1e+97, 1e+98, 1e+99, 1e+100,
	1e+101, 1e+102, 1e+103, 1e+104, 1e+105, 1e+106, 1e+107, 1e+108, 1e+109,
	1e+110, 1e+111, 1e+112, 1e+113, 1e+114, 1e+115, 1e+116, 1e+117, 1e+118,
	1e+119, 1e+120, 1e+121, 1e+122, 1e+123, 1e+124, 1e+125, 1e+126, 1e+127,
	1e-128, 1e-127, 1e-126, 1e-125, 1e-124, 1e-123, 1e-122, 1e-121, 1e-120,
	1e-119, 1e-118, 1e-117, 1e-116, 1e-115, 1e-114, 1e-113, 1e-112, 1e-111,
	1e-110, 1e-109, 1e-108, 1e-107, 1e-106, 1e-105, 1e-104, 1e-103, 1e-102,
	1e-101, 1e-100, 1e-99, 1e-98, 1e-97, 1e-96,
	1e-95, 1e-94, 1e-93, 1e-92, 1e-91, 1e-90, 1e-89, 1e-88, 1e-87, 1e-86,
	1e-85, 1e-84, 1e-83, 1e-82, 1e-81, 1e-80, 1e-79, 1e-78, 1e-77, 1e-76,
	1e-75, 1e-74, 1e-73, 1e-72, 1e-71, 1e-70, 1e-69, 1e-68, 1e-67, 1e-66,
	1e-65, 1e-64, 1e-63, 1e-62, 1e-61, 1e-60, 1e-59, 1e-58, 1e-57, 1e-56,
	1e-55, 1e-54, 1e-53, 1e-52, 1e-51, 1e-50, 1e-49, 1e-48, 1e-47, 1e-46,
	1e-45, 1e-44, 1e-43, 1e-42, 1e-41, 1e-40, 1e-39, 1e-38, 1e-37, 1e-36,
	1e-35, 1e-34, 1e-33, 1e-32, 1e-31, 1e-30, 1e-29, 1e-28, 1e-27, 1e-26,
	1e-25, 1e-24, 1e-23, 1e-22, 1e-21, 1e-20, 1e-19, 1e-18, 1e-17, 1e-16,
	1e-15, 1e-14, 1e-13, 1e-12, 1e-11, 1e-10, 1e-09, 1e-08, 1e-07, 1e-06,
	1e-05, 0.0001, 0.001, 0.01, 0.1,
}

// A Bracket is a part of a cumulative distribution.
type bin struct {
	count uint64
	val   int8
	exp   int8
}

func newBinRaw(val int8, exp int8, count uint64) *bin {
	return &bin{
		count: count,
		val:   val,
		exp:   exp,
	}
}

func newBin() *bin {
	return newBinRaw(0, 0, 0)
}

func newBinFromFloat64(d float64) *bin {
	hb := newBinRaw(0, 0, 0)
	hb.setFromFloat64(d)
	return hb
}

type fastL2 struct {
	l1, l2 int
}

func (hb *bin) newFastL2() fastL2 {
	return fastL2{l1: int(uint8(hb.exp)), l2: int(uint8(hb.val))}
}

func (hb *bin) setFromFloat64(d float64) *bin {
	hb.val = -1
	if math.IsInf(d, 0) || math.IsNaN(d) {
		return hb
	}
	if d == 0.0 {
		hb.val = 0
		return hb
	}
	sign := 1
	if math.Signbit(d) {
		sign = -1
	}
	d = math.Abs(d)
	big_exp := int(math.Floor(math.Log10(d)))
	hb.exp = int8(big_exp)
	if int(hb.exp) != big_exp { //rolled
		hb.exp = 0
		if big_exp < 0 {
			hb.val = 0
		}
		return hb
	}
	d = d / hb.powerOfTen()
	d = d * 10
	hb.val = int8(sign * int(math.Floor(d+1e-13)))
	if hb.val == 100 || hb.val == -100 {
		if hb.exp < 127 {
			hb.val = hb.val / 10
			hb.exp++
		} else {
			hb.val = 0
			hb.exp = 0
		}
	}
	if hb.val == 0 {
		hb.exp = 0
		return hb
	}
	if !((hb.val >= 10 && hb.val < 100) ||
		(hb.val <= -10 && hb.val > -100)) {
		hb.val = -1
		hb.exp = 0
	}
	return hb
}

func (hb *bin) powerOfTen() float64 {
	idx := int(uint8(hb.exp))
	return powerOfTen[idx]
}

func (hb *bin) isNaN() bool {
	// aval := abs(hb.val)
	aval := hb.val
	if aval < 0 {
		aval = -aval
	}
	if 99 < aval { // in [100... ]: nan
		return true
	}
	if 9 < aval { // in [10 - 99]: valid range
		return false
	}
	if 0 < aval { // in [1  - 9 ]: nan
		return true
	}
	if 0 == aval { // in [0]      : zero bucket
		return false
	}
	return false
}

func (hb *bin) value() float64 {
	if hb.isNaN() {
		return math.NaN()
	}
	if hb.val < 10 && hb.val > -10 {
		return 0.0
	}
	return (float64(hb.val) / 10.0) * hb.powerOfTen()
}

func (hb *bin) binWidth() float64 {
	if hb.isNaN() {
		return math.NaN()
	}
	if hb.val < 10 && hb.val > -10 {
		return 0.0
	}
	return hb.powerOfTen() / 10.0
}

func (hb *bin) midpoint() float64 {
	if hb.isNaN() {
		return math.NaN()
	}
	out := hb.value()
	if out == 0 {
		return 0
	}
	interval := hb.binWidth()
	if out < 0 {
		interval = interval * -1
	}
	return out + interval/2.0
}

func (hb *bin) left() float64 {
	if hb.isNaN() {
		return math.NaN()
	}
	out := hb.value()
	if out >= 0 {
		return out
	}
	return out - hb.binWidth()
}

func (h1 *bin) compare(h2 *bin) int {
	var v1, v2 int

	// 1) slide exp positive
	// 2) shift by size of val multiple by (val != 0)
	// 3) then add or subtract val accordingly

	if h1.val >= 0 {
		v1 = ((int(h1.exp)+256)<<8)*int(((int(h1.val)|(^int(h1.val)+1))>>8)&1) + int(h1.val)
	} else {
		v1 = ((int(h1.exp)+256)<<8)*int(((int(h1.val)|(^int(h1.val)+1))>>8)&1) - int(h1.val)
	}

	if h2.val >= 0 {
		v2 = ((int(h2.exp)+256)<<8)*int(((int(h2.val)|(^int(h2.val)+1))>>8)&1) + int(h2.val)
	} else {
		v2 = ((int(h2.exp)+256)<<8)*int(((int(h2.val)|(^int(h2.val)+1))>>8)&1) - int(h2.val)
	}

	// return the difference
	return v2 - v1
}

// This histogram structure tracks values are two decimal digits of precision
// with a bounded error that remains bounded upon composition
type Histogram struct {
	bvs    []bin
	used   uint16
	allocd uint16

	lookup [256][]uint16

	mutex    sync.RWMutex
	useLocks bool
}

const (
	BVL1, BVL1MASK uint64 = iota, 0xff << (8 * iota)
	BVL2, BVL2MASK
	BVL3, BVL3MASK
	BVL4, BVL4MASK
	BVL5, BVL5MASK
	BVL6, BVL6MASK
	BVL7, BVL7MASK
	BVL8, BVL8MASK
)

func getBytesRequired(val uint64) (len int8) {
	if 0 != (BVL8MASK|BVL7MASK|BVL6MASK|BVL5MASK)&val {
		if 0 != BVL8MASK&val {
			return int8(BVL8)
		}
		if 0 != BVL7MASK&val {
			return int8(BVL7)
		}
		if 0 != BVL6MASK&val {
			return int8(BVL6)
		}
		if 0 != BVL5MASK&val {
			return int8(BVL5)
		}
	} else {
		if 0 != BVL4MASK&val {
			return int8(BVL4)
		}
		if 0 != BVL3MASK&val {
			return int8(BVL3)
		}
		if 0 != BVL2MASK&val {
			return int8(BVL2)
		}
	}
	return int8(BVL1)
}

func writeBin(out io.Writer, in bin, idx int) (err error) {

	err = binary.Write(out, binary.BigEndian, in.val)
	if err != nil {
		return
	}

	err = binary.Write(out, binary.BigEndian, in.exp)
	if err != nil {
		return
	}

	var tgtType int8 = getBytesRequired(in.count)

	err = binary.Write(out, binary.BigEndian, tgtType)
	if err != nil {
		return
	}

	var bcount = make([]uint8, 8)
	b := bcount[0 : tgtType+1]
	for i := tgtType; i >= 0; i-- {
		b[i] = uint8(uint64(in.count>>(uint8(i)*8)) & 0xff)
	}

	err = binary.Write(out, binary.BigEndian, b)
	if err != nil {
		return
	}
	return
}

func readBin(in io.Reader) (out bin, err error) {
	err = binary.Read(in, binary.BigEndian, &out.val)
	if err != nil {
		return
	}

	err = binary.Read(in, binary.BigEndian, &out.exp)
	if err != nil {
		return
	}
	var bvl uint8
	err = binary.Read(in, binary.BigEndian, &bvl)
	if err != nil {
		return
	}
	if bvl > uint8(BVL8) {
		return out, errors.New("encoding error: bvl value is greater than max allowable")
	}

	bcount := make([]byte, 8)
	b := bcount[0 : bvl+1]
	err = binary.Read(in, binary.BigEndian, b)
	if err != nil {
		return
	}

	var count uint64 = 0
	for i := int(bvl + 1); i >= 0; i-- {
		count |= (uint64(bcount[i]) << (uint8(i) * 8))
	}

	out.count = count
	return
}

func Deserialize(in io.Reader) (h *Histogram, err error) {
	h = New()
	if h.bvs == nil {
		h.bvs = make([]bin, 0, defaultHistSize)
	}

	var nbin int16
	err = binary.Read(in, binary.BigEndian, &nbin)
	if err != nil {
		return
	}

	for ii := int16(0); ii < nbin; ii++ {
		bb, err := readBin(in)
		if err != nil {
			return h, err
		}
		h.insertBin(&bb, int64(bb.count))
	}
	return h, nil
}

func (h *Histogram) Serialize(w io.Writer) error {

	var nbin int16 = int16(len(h.bvs))
	if err := binary.Write(w, binary.BigEndian, nbin); err != nil {
		return err
	}

	for i := 0; i < len(h.bvs); i++ {
		if err := writeBin(w, h.bvs[i], i); err != nil {
			return err
		}
	}
	return nil
}

func (h *Histogram) SerializeB64(w io.Writer) error {
	buf := bytes.NewBuffer([]byte{})
	h.Serialize(buf)

	encoder := base64.NewEncoder(base64.StdEncoding, w)
	if _, err := encoder.Write(buf.Bytes()); err != nil {
		return err
	}
	encoder.Close()
	return nil
}

// New returns a new Histogram
func New() *Histogram {
	return &Histogram{
		allocd:   defaultHistSize,
		used:     0,
		bvs:      make([]bin, defaultHistSize),
		useLocks: true,
	}
}

// New returns a Histogram without locking
func NewNoLocks() *Histogram {
	return &Histogram{
		allocd:   defaultHistSize,
		used:     0,
		bvs:      make([]bin, defaultHistSize),
		useLocks: false,
	}
}

// NewFromStrings returns a Histogram created from DecStrings strings
func NewFromStrings(strs []string, locks bool) (*Histogram, error) {

	bin, err := stringsToBin(strs)
	if err != nil {
		return nil, err
	}

	return newFromBins(bin, locks), nil
}

// NewFromBins returns a Histogram created from a bins struct slice
func newFromBins(bins []bin, locks bool) *Histogram {
	return &Histogram{
		allocd:   uint16(len(bins) + 10), // pad it with 10
		used:     uint16(len(bins)),
		bvs:      bins,
		useLocks: locks,
	}
}

// Max returns the approximate maximum recorded value.
func (h *Histogram) Max() float64 {
	return h.ValueAtQuantile(1.0)
}

// Min returns the approximate minimum recorded value.
func (h *Histogram) Min() float64 {
	return h.ValueAtQuantile(0.0)
}

// Mean returns the approximate arithmetic mean of the recorded values.
func (h *Histogram) Mean() float64 {
	return h.ApproxMean()
}

// Reset forgets all bins in the histogram (they remain allocated)
func (h *Histogram) Reset() {
	if h.useLocks {
		h.mutex.Lock()
		defer h.mutex.Unlock()
	}
	for i := 0; i < 256; i++ {
		if h.lookup[i] != nil {
			for j := range h.lookup[i] {
				h.lookup[i][j] = 0
			}
		}
	}
	h.used = 0
}

// RecordIntScale records an integer scaler value, returning an error if the
// value is out of range.
func (h *Histogram) RecordIntScale(val, scale int) error {
	return h.RecordIntScales(val, scale, 1)
}

// RecordValue records the given value, returning an error if the value is out
// of range.
func (h *Histogram) RecordValue(v float64) error {
	return h.RecordValues(v, 1)
}

// RecordCorrectedValue records the given value, correcting for stalls in the
// recording process. This only works for processes which are recording values
// at an expected interval (e.g., doing jitter analysis). Processes which are
// recording ad-hoc values (e.g., latency for incoming requests) can't take
// advantage of this.
// CH Compat
func (h *Histogram) RecordCorrectedValue(v, expectedInterval int64) error {
	if err := h.RecordValue(float64(v)); err != nil {
		return err
	}

	if expectedInterval <= 0 || v <= expectedInterval {
		return nil
	}

	missingValue := v - expectedInterval
	for missingValue >= expectedInterval {
		if err := h.RecordValue(float64(missingValue)); err != nil {
			return err
		}
		missingValue -= expectedInterval
	}

	return nil
}

// find where a new bin should go
func (h *Histogram) internalFind(hb *bin) (bool, uint16) {
	if h.used == 0 {
		return false, 0
	}
	f2 := hb.newFastL2()
	if h.lookup[f2.l1] != nil {
		if idx := h.lookup[f2.l1][f2.l2]; idx != 0 {
			return true, idx - 1
		}
	}
	rv := -1
	idx := uint16(0)
	l := int(0)
	r := int(h.used - 1)
	for l < r {
		check := (r + l) / 2
		rv = h.bvs[check].compare(hb)
		if rv == 0 {
			l = check
			r = check
		} else if rv > 0 {
			l = check + 1
		} else {
			r = check - 1
		}
	}
	if rv != 0 {
		rv = h.bvs[l].compare(hb)
	}
	idx = uint16(l)
	if rv == 0 {
		return true, idx
	}
	if rv < 0 {
		return false, idx
	}
	idx++
	return false, idx
}

func (h *Histogram) insertBin(hb *bin, count int64) uint64 {
	if h.useLocks {
		h.mutex.Lock()
		defer h.mutex.Unlock()
	}
	found, idx := h.internalFind(hb)
	if !found {
		if h.used == h.allocd {
			new_bvs := make([]bin, h.allocd+defaultHistSize)
			if idx > 0 {
				copy(new_bvs[0:], h.bvs[0:idx])
			}
			if idx < h.used {
				copy(new_bvs[idx+1:], h.bvs[idx:])
			}
			h.allocd = h.allocd + defaultHistSize
			h.bvs = new_bvs
		} else {
			copy(h.bvs[idx+1:], h.bvs[idx:h.used])
		}
		h.bvs[idx].val = hb.val
		h.bvs[idx].exp = hb.exp
		h.bvs[idx].count = uint64(count)
		h.used++
		for i := idx; i < h.used; i++ {
			f2 := h.bvs[i].newFastL2()
			if h.lookup[f2.l1] == nil {
				h.lookup[f2.l1] = make([]uint16, 256)
			}
			h.lookup[f2.l1][f2.l2] = uint16(i) + 1
		}
		return h.bvs[idx].count
	}
	var newval uint64
	if count >= 0 {
		newval = h.bvs[idx].count + uint64(count)
	} else {
		newval = h.bvs[idx].count - uint64(-count)
	}
	if newval < h.bvs[idx].count { //rolled
		newval = ^uint64(0)
	}
	h.bvs[idx].count = newval
	return newval - h.bvs[idx].count
}

// RecordIntScales records n occurrences of the given value, returning an error if
// the value is out of range.
func (h *Histogram) RecordIntScales(val, scale int, n int64) error {
	sign := 1
	if val == 0 {
		scale = 0
	} else {
		if val < 0 {
			val = 0 - val
			sign = -1
		}
		if val < 10 {
			val *= 10
			scale -= 1
		}
		for val >= 100 {
			val /= 10
			scale++
		}
	}
	if scale < -128 {
		val = 0
		scale = 0
	} else if scale > 127 {
		val = 0xff
		scale = 0
	}
	val *= sign
	hb := bin{val: int8(val), exp: int8(scale), count: 0}
	h.insertBin(&hb, n)
	return nil
}

// RecordValues records n occurrences of the given value, returning an error if
// the value is out of range.
func (h *Histogram) RecordValues(v float64, n int64) error {
	var hb bin
	hb.setFromFloat64(v)
	h.insertBin(&hb, n)
	return nil
}

// Approximate mean
func (h *Histogram) ApproxMean() float64 {
	if h.useLocks {
		h.mutex.RLock()
		defer h.mutex.RUnlock()
	}
	divisor := 0.0
	sum := 0.0
	for i := uint16(0); i < h.used; i++ {
		midpoint := h.bvs[i].midpoint()
		cardinality := float64(h.bvs[i].count)
		divisor += cardinality
		sum += midpoint * cardinality
	}
	if divisor == 0.0 {
		return math.NaN()
	}
	return sum / divisor
}

// Approximate sum
func (h *Histogram) ApproxSum() float64 {
	if h.useLocks {
		h.mutex.RLock()
		defer h.mutex.RUnlock()
	}
	sum := 0.0
	for i := uint16(0); i < h.used; i++ {
		midpoint := h.bvs[i].midpoint()
		cardinality := float64(h.bvs[i].count)
		sum += midpoint * cardinality
	}
	return sum
}

func (h *Histogram) ApproxQuantile(q_in []float64) ([]float64, error) {
	if h.useLocks {
		h.mutex.RLock()
		defer h.mutex.RUnlock()
	}
	q_out := make([]float64, len(q_in))
	i_q, i_b := 0, uint16(0)
	total_cnt, bin_width, bin_left, lower_cnt, upper_cnt := 0.0, 0.0, 0.0, 0.0, 0.0
	if len(q_in) == 0 {
		return q_out, nil
	}
	// Make sure the requested quantiles are in order
	for i_q = 1; i_q < len(q_in); i_q++ {
		if q_in[i_q-1] > q_in[i_q] {
			return nil, errors.New("out of order")
		}
	}
	// Add up the bins
	for i_b = 0; i_b < h.used; i_b++ {
		if !h.bvs[i_b].isNaN() {
			total_cnt += float64(h.bvs[i_b].count)
		}
	}
	if total_cnt == 0.0 {
		return nil, errors.New("empty_histogram")
	}

	for i_q = 0; i_q < len(q_in); i_q++ {
		if q_in[i_q] < 0.0 || q_in[i_q] > 1.0 {
			return nil, errors.New("out of bound quantile")
		}
		q_out[i_q] = total_cnt * q_in[i_q]
	}

	for i_b = 0; i_b < h.used; i_b++ {
		if h.bvs[i_b].isNaN() {
			continue
		}
		bin_width = h.bvs[i_b].binWidth()
		bin_left = h.bvs[i_b].left()
		lower_cnt = upper_cnt
		upper_cnt = lower_cnt + float64(h.bvs[i_b].count)
		break
	}
	for i_q = 0; i_q < len(q_in); i_q++ {
		for i_b < (h.used-1) && upper_cnt < q_out[i_q] {
			i_b++
			bin_width = h.bvs[i_b].binWidth()
			bin_left = h.bvs[i_b].left()
			lower_cnt = upper_cnt
			upper_cnt = lower_cnt + float64(h.bvs[i_b].count)
		}
		if lower_cnt == q_out[i_q] {
			q_out[i_q] = bin_left
		} else if upper_cnt == q_out[i_q] {
			q_out[i_q] = bin_left + bin_width
		} else {
			if bin_width == 0 {
				q_out[i_q] = bin_left
			} else {
				q_out[i_q] = bin_left + (q_out[i_q]-lower_cnt)/(upper_cnt-lower_cnt)*bin_width
			}
		}
	}
	return q_out, nil
}

// ValueAtQuantile returns the recorded value at the given quantile (0..1).
func (h *Histogram) ValueAtQuantile(q float64) float64 {
	if h.useLocks {
		h.mutex.RLock()
		defer h.mutex.RUnlock()
	}
	q_in := make([]float64, 1)
	q_in[0] = q
	q_out, err := h.ApproxQuantile(q_in)
	if err == nil && len(q_out) == 1 {
		return q_out[0]
	}
	return math.NaN()
}

// SignificantFigures returns the significant figures used to create the
// histogram
// CH Compat
func (h *Histogram) SignificantFigures() int64 {
	return 2
}

// Equals returns true if the two Histograms are equivalent, false if not.
func (h *Histogram) Equals(other *Histogram) bool {
	if h.useLocks {
		h.mutex.RLock()
		defer h.mutex.RUnlock()
	}
	if other.useLocks {
		other.mutex.RLock()
		defer other.mutex.RUnlock()
	}
	switch {
	case
		h.used != other.used:
		return false
	default:
		for i := uint16(0); i < h.used; i++ {
			if h.bvs[i].compare(&other.bvs[i]) != 0 {
				return false
			}
			if h.bvs[i].count != other.bvs[i].count {
				return false
			}
		}
	}
	return true
}

func (h *Histogram) CopyAndReset() *Histogram {
	if h.useLocks {
		h.mutex.Lock()
		defer h.mutex.Unlock()
	}
	newhist := &Histogram{
		allocd: h.allocd,
		used:   h.used,
		bvs:    h.bvs,
	}
	h.allocd = defaultHistSize
	h.bvs = make([]bin, defaultHistSize)
	h.used = 0
	for i := 0; i < 256; i++ {
		if h.lookup[i] != nil {
			for j := range h.lookup[i] {
				h.lookup[i][j] = 0
			}
		}
	}
	return newhist
}
func (h *Histogram) DecStrings() []string {
	if h.useLocks {
		h.mutex.Lock()
		defer h.mutex.Unlock()
	}
	out := make([]string, h.used)
	for i, bin := range h.bvs[0:h.used] {
		var buffer bytes.Buffer
		buffer.WriteString("H[")
		buffer.WriteString(fmt.Sprintf("%3.1e", bin.value()))
		buffer.WriteString("]=")
		buffer.WriteString(fmt.Sprintf("%v", bin.count))
		out[i] = buffer.String()
	}
	return out
}

// takes the output of DecStrings and deserializes it into a Bin struct slice
func stringsToBin(strs []string) ([]bin, error) {

	bins := make([]bin, len(strs))
	for i, str := range strs {

		// H[0.0e+00]=1

		// H[0.0e+00]= <1>
		countString := strings.Split(str, "=")[1]
		countInt, err := strconv.ParseInt(countString, 10, 64)
		if err != nil {
			return nil, err
		}

		// H[ <0.0> e+00]=1
		valString := strings.Split(strings.Split(strings.Split(str, "=")[0], "e")[0], "[")[1]
		valInt, err := strconv.ParseFloat(valString, 64)
		if err != nil {
			return nil, err
		}

		// H[0.0e <+00> ]=1
		expString := strings.Split(strings.Split(strings.Split(str, "=")[0], "e")[1], "]")[0]
		expInt, err := strconv.ParseInt(expString, 10, 8)
		if err != nil {
			return nil, err
		}
		bins[i] = *newBinRaw(int8(valInt*10), int8(expInt), uint64(countInt))
	}

	return bins, nil
}

// UnmarshalJSON - histogram will come in a base64 encoded serialized form
func (h *Histogram) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	h, err = Deserialize(bytes.NewBuffer(data))
	return err
}

func (h *Histogram) MarshalJSON() ([]byte, error) {
	buf := bytes.NewBuffer([]byte{})
	err := h.SerializeB64(buf)
	if err != nil {
		return buf.Bytes(), err
	}
	return json.Marshal(buf.String())
}
