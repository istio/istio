// Copyright 2017 Istio Authors
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

package memquota

// Implements a rolling window that allows N units to be allocated per rolling time interval.
// Time is abstracted in terms of ticks, provided by the caller, decoupling the
// implementation from real-time, enabling much easier testing and more flexibility.
type rollingWindow struct {
	// one slot per tick in the window, tracking consumption for that tick
	slots []int64

	// slot where consumption for the currentSlotTick is recorded
	currentSlot int

	// the tick count associated with the current slot
	currentSlotTick int64

	// the total # of units currently available in the window
	avail int64
}

// Creates a new rolling window tracker used to implement rate limiting.
//
// The limit parameter determines the maximum amount that can be allocated within the
// window.
//
// The ticksPerWindow parameter determines the number of quantized time intervals within the window.
// When quota is allocated, it belongs to the current tick. As time marches on,
// the quota associated with older ticks is reclaimed.
func newRollingWindow(limit int64, ticksInWindow int64) *rollingWindow {
	return &rollingWindow{
		avail: limit,
		slots: make([]int64, ticksInWindow),
	}
}

func (w *rollingWindow) alloc(amount int64, currentTick int64) bool {
	w.roll(currentTick)

	if amount > w.avail {
		// not enough room
		return false
	}

	// now record the units being allocated
	w.slots[w.currentSlot] += amount
	w.avail -= amount

	return true
}

func (w *rollingWindow) release(amount int64, currentTick int64) int64 {
	w.roll(currentTick)

	// we release from the leading edge of the window moving back in time
	// until we've released enough or released everything, whichever is first...
	var total int64
	index := w.currentSlot
	for range w.slots {
		avail := w.slots[index]

		if avail >= amount {
			w.slots[index] -= amount
			total += amount
			break
		}

		w.slots[index] = 0
		total += avail
		amount -= avail

		index--
		if index < 0 {
			index = len(w.slots) - 1
		}
	}

	w.avail += total
	return total
}

func (w *rollingWindow) roll(currentTick int64) {
	// how many ticks has the window moved since we were last here?
	behind := int(currentTick - w.currentSlotTick)
	if behind > len(w.slots) {
		behind = len(w.slots)
	}

	// reclaim any units that are now outside of the window
	for i := 0; i < behind; i++ {
		index := (w.currentSlot + 1 + i) % len(w.slots)
		w.avail += w.slots[index]
		w.slots[index] = 0
	}

	w.currentSlot = (w.currentSlot + behind) % len(w.slots)
	w.currentSlotTick = currentTick
}

func (w *rollingWindow) available() int64 {
	return w.avail
}
