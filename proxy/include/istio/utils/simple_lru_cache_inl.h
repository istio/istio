/* Copyright 2017 Istio Authors. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// A generic LRU cache that maps from type Key to Value*.
//
// . Memory usage is fairly high: on a 64-bit architecture, a cache with
//   8-byte keys can use 108 bytes per element, not counting the
//   size of the values.  This overhead can be significant if many small
//   elements are stored in the cache.
//
// . Lookup returns a "Value*".  Client should call "Release" when done.
//
// . Override "RemoveElement" if you want to be notified when an
//   element is being removed. The default implementation simply calls
//   "delete" on the pointer.
//
// . Call Clear() before destruction.
//
// . No internal locking is done: if the same cache will be shared
//   by multiple threads, the caller should perform the required
//   synchronization before invoking any operations on the cache.
//   Note a reader lock is not sufficient as Lookup() updates the pin count.
//
// . We provide support for setting a "max_idle_time".  Entries
//   are discarded when they have not been used for a time
//   greater than the specified max idle time.  If you do not
//   call SetMaxIdleSeconds(), entries never expire (they can
//   only be removed to meet size constraints).
//
// . We also provide support for a strict age-based eviction policy
//   instead of LRU.  See SetAgeBasedEviction().

#ifndef ISTIO_UTILS_SIMPLE_LRU_CACHE_INL_H_
#define ISTIO_UTILS_SIMPLE_LRU_CACHE_INL_H_

#include <stddef.h>
#include <sys/time.h>
#include <cassert>
#include <cmath>
#include <limits>
#include <sstream>
#include <string>
#include <unordered_map>
#include <utility>

#include "google_macros.h"
#include "simple_lru_cache.h"

namespace istio {
namespace utils {

// Define number of microseconds for a second.
const int64_t kSecToUsec = 1000000;

// Define a simple cycle timer interface to encapsulate timer related code.
// The concept is from CPU cycle. The cycle clock code from
// https://github.com/google/benchmark/blob/master/src/cycleclock.h can be used.
// But that code only works for some platforms. To make code works for all
// platforms, SimpleCycleTimer class uses a fake CPU cycle each taking a
// microsecond. If needed, this timer class can be easily replaced by a
// real cycle_clock.
class SimpleCycleTimer {
 public:
  // Return the current cycle in microseconds.
  static int64_t Now() {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return static_cast<int64_t>(tv.tv_sec * kSecToUsec + tv.tv_usec);
  }
  // Return number of cycles in a second.
  static int64_t Frequency() { return kSecToUsec; }

 private:
  SimpleCycleTimer();  // no instances
};

// A constant iterator. a client of SimpleLRUCache should not create these
// objects directly, instead, create objects of type
// SimpleLRUCache::const_iterator.  This is created inside of
// SimpleLRUCache::begin(),end().  Key and Value are the same as the template
// args to SimpleLRUCache Elem - the Value type for the internal hash_map that
// the SimpleLRUCache maintains H and EQ are the same as the template arguments
// for SimpleLRUCache
//
// NOTE: the iterator needs to keep a copy of end() for the Cache it is
// iterating over this is so SimpleLRUCacheConstIterator does not try to update
// its internal pair<Key, Value*> if its internal hash_map iterator is pointing
// to end see the implementation of operator++ for an example.
//
// NOTE: DO NOT SAVE POINTERS TO THE ITEM RETURNED BY THIS ITERATOR
// e.g. SimpleLRUCacheConstIterator it = something; do not say KeyToSave
// &something->first this will NOT work., as soon as you increment the iterator
// this will be gone. :(

template <class Key, class Value, class MapType>
class SimpleLRUCacheConstIterator
    : public std::iterator<std::input_iterator_tag,
                           const std::pair<Key, Value*>> {
 public:
  typedef typename MapType::const_iterator HashMapConstIterator;
  // Allow parent template's types to be referenced without qualification.
  typedef typename SimpleLRUCacheConstIterator::reference reference;
  typedef typename SimpleLRUCacheConstIterator::pointer pointer;

  // This default constructed Iterator can only be assigned to or destroyed.
  // All other operations give undefined behaviour.
  SimpleLRUCacheConstIterator() {}
  SimpleLRUCacheConstIterator(HashMapConstIterator it,
                              HashMapConstIterator end);
  SimpleLRUCacheConstIterator& operator++();

  reference operator*() { return external_view_; }
  pointer operator->() { return &external_view_; }

  // For LRU mode, last_use_time() returns elements last use time.
  // See GetLastUseTime() description for more information.
  int64_t last_use_time() const { return last_use_; }

  // For age-based mode, insertion_time() returns elements insertion time.
  // See GetInsertionTime() description for more information.
  int64_t insertion_time() const { return last_use_; }

  friend bool operator==(const SimpleLRUCacheConstIterator& a,
                         const SimpleLRUCacheConstIterator& b) {
    return a.it_ == b.it_;
  }

  friend bool operator!=(const SimpleLRUCacheConstIterator& a,
                         const SimpleLRUCacheConstIterator& b) {
    return !(a == b);
  }

 private:
  HashMapConstIterator it_;
  HashMapConstIterator end_;
  std::pair<Key, Value*> external_view_;
  int64_t last_use_;
};

// Each entry uses the following structure
template <typename Key, typename Value>
struct SimpleLRUCacheElem {
  Key key;                             // The key
  Value* value;                        // The stored value
  int pin;                             // Number of outstanding releases
  size_t units;                        // Number of units for this value
  SimpleLRUCacheElem* next = nullptr;  // Next entry in LRU chain
  SimpleLRUCacheElem* prev = nullptr;  // Prev entry in LRU chain
  int64_t last_use_;                   // Timestamp of last use (in LRU mode)
  //     or creation (in age-based mode)

  SimpleLRUCacheElem(const Key& k, Value* v, int p, size_t u, int64_t last_use)
      : key(k), value(v), pin(p), units(u), last_use_(last_use) {}

  bool IsLinked() const {
    // If we are in the LRU then next and prev should be non-NULL. Otherwise
    // both should be properly initialized to nullptr.
    assert(static_cast<bool>(next == nullptr) ==
           static_cast<bool>(prev == nullptr));
    return next != nullptr;
  }

  void Unlink() {
    if (!IsLinked()) return;
    prev->next = next;
    next->prev = prev;
    prev = nullptr;
    next = nullptr;
  }

  void Link(SimpleLRUCacheElem* head) {
    next = head->next;
    prev = head;
    next->prev = this;  // i.e. head->next->prev = this;
    prev->next = this;  // i.e. head->next = this;
  }
  static const int64_t kNeverUsed = -1;
};

template <typename Key, typename Value>
const int64_t SimpleLRUCacheElem<Key, Value>::kNeverUsed;

// A simple class passed into various cache methods to change the
// behavior for that single call.
class SimpleLRUCacheOptions {
 public:
  SimpleLRUCacheOptions() : update_eviction_order_(true) {}

  // If false neither the last modified time (for based age eviction) nor
  // the element ordering (for LRU eviction) will be updated.
  // This value must be the same for both Lookup and Release.
  // The default is true.
  bool update_eviction_order() const { return update_eviction_order_; }
  void set_update_eviction_order(bool v) { update_eviction_order_ = v; }

 private:
  bool update_eviction_order_;
};

// The MapType's value_type must be pair<const Key, Elem*>
template <class Key, class Value, class MapType, class EQ>
class SimpleLRUCacheBase {
 public:
  // class ScopedLookup
  // If you have some code that looks like this:
  // val = c->Lookup(key);
  // if (val) {
  //   if (something) {
  //     c->Release(key, val);
  //     return;
  //   }
  //   if (something else) {
  //     c->Release(key, val);
  //     return;
  //   }
  // Then ScopedLookup will make the code simpler.  It automatically
  // releases the value when the instance goes out of scope.
  // Example:
  //   ScopedLookup lookup(c, key);
  //   if (lookup.Found()) {
  //     ...
  //
  // NOTE:  Be extremely careful when using ScopedLookup with Mutexes.  This
  // code is safe since the lock will be released after the ScopedLookup is
  // destroyed.
  //   MutexLock l(&mu_);
  //   ScopedLookup lookup(....);
  //
  // This is NOT safe since the lock is released before the ScopedLookup is
  // destroyed, and consequently the value will be unpinned without the lock
  // being held.
  //   mu_.Lock();
  //   ScopedLookup lookup(....);
  //   ...
  //   mu_.Unlock();
  class ScopedLookup {
   public:
    ScopedLookup(SimpleLRUCacheBase* cache, const Key& key)
        : cache_(cache),
          key_(key),
          value_(cache_->LookupWithOptions(key_, options_)) {}

    ScopedLookup(SimpleLRUCacheBase* cache, const Key& key,
                 const SimpleLRUCacheOptions& options)
        : cache_(cache),
          key_(key),
          options_(options),
          value_(cache_->LookupWithOptions(key_, options_)) {}

    ~ScopedLookup() {
      if (value_ != nullptr) cache_->ReleaseWithOptions(key_, value_, options_);
    }
    const Key& key() const { return key_; }
    Value* value() const { return value_; }
    bool Found() const { return value_ != nullptr; }
    const SimpleLRUCacheOptions& options() const { return options_; }

   private:
    SimpleLRUCacheBase* const cache_;
    const Key key_;
    const SimpleLRUCacheOptions options_;
    Value* const value_;

    GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(ScopedLookup);
  };

  // Create a cache that will hold up to the specified number of units.
  // Usually the units will be byte sizes, but in some caches different
  // units may be used.  For instance, we may want each open file to
  // be one unit in an open-file cache.
  //
  // By default, the max_idle_time is infinity; i.e. entries will
  // stick around in the cache regardless of how old they are.
  explicit SimpleLRUCacheBase(int64_t total_units);

  // Release all resources.  Cache must have been "Clear"ed.  This
  // requirement is imposed because "Clear()" will call
  // "RemoveElement" for each element in the cache.  The destructor
  // cannot do that because it runs after any subclass destructor.
  virtual ~SimpleLRUCacheBase() {
    assert(table_.size() == 0);
    assert(defer_.size() == 0);
  }

  // Change the maximum size of the cache to the specified number of units.
  // If necessary, entries will be evicted to comply with the new size.
  void SetMaxSize(int64_t total_units) {
    max_units_ = total_units;
    GarbageCollect();
  }

  // Change the max idle time to the specified number of seconds.
  // If "seconds" is a negative number, it sets the max idle time
  // to infinity.
  void SetMaxIdleSeconds(double seconds) {
    SetTimeout(seconds, true /* lru */);
  }

  // Stop using the LRU eviction policy and instead expire anything
  // that has been in the cache for more than the specified number
  // of seconds.
  // If "seconds" is a negative number, entries don't expire but if
  // we need to make room the oldest entries will be removed first.
  // You can't set both a max idle time and age-based eviction.
  void SetAgeBasedEviction(double seconds) {
    SetTimeout(seconds, false /* lru */);
  }

  // If cache contains an entry for "k", return a pointer to it.
  // Else return nullptr.
  //
  // If a value is returned, the caller must call "Release" when it no
  // longer needs that value.  This functionality is useful to prevent
  // the value from being evicted from the cache until it is no longer
  // being used.
  Value* Lookup(const Key& k) {
    return LookupWithOptions(k, SimpleLRUCacheOptions());
  }

  // Same as "Lookup(Key)" but allows for additional options.  See
  // the SimpleLRUCacheOptions object for more information.
  Value* LookupWithOptions(const Key& k, const SimpleLRUCacheOptions& options);

  // Removes the pinning done by an earlier "Lookup".  After this call,
  // the caller should no longer depend on the value sticking around.
  //
  // If there are no more pins on this entry, it may be deleted if
  // either it has been "Remove"d, or the cache is overfull.
  // In this case "RemoveElement" will be called.
  void Release(const Key& k, Value* value) {
    ReleaseWithOptions(k, value, SimpleLRUCacheOptions());
  }

  // Same as "Release(Key, Value)" but allows for additional options.  See
  // the SimpleLRUCacheOptions object for more information.  Take care
  // that the SimpleLRUCacheOptions object passed into this method is
  // compatible with SimpleLRUCacheOptions object passed into Lookup.
  // If they are incompatible it can put the cache into some unexpected
  // states.  Better yet, just use a ScopedLookup which takes care of this
  // for you.
  void ReleaseWithOptions(const Key& k, Value* value,
                          const SimpleLRUCacheOptions& options);

  // Insert the specified "k,value" pair in the cache.  Remembers that
  // the value occupies "units" units.  For "InsertPinned", the newly
  // inserted value will be pinned in the cache: the caller should
  // call "Release" when it wants to remove the pin.
  //
  // Any old entry for "k" is "Remove"d.
  //
  // If the insertion causes the cache to become overfull, unpinned
  // entries will be deleted in an LRU order to make room.
  // "RemoveElement" will be called for each such entry.
  void Insert(const Key& k, Value* value, size_t units) {
    InsertPinned(k, value, units);
    Release(k, value);
  }
  void InsertPinned(const Key& k, Value* value, size_t units);

  // Change the reported size of an object.
  void UpdateSize(const Key& k, const Value* value, size_t units);

  // return true iff <k, value> pair is still in use
  // (i.e., either in the table or the deferred list)
  // Note, if (value == nullptr), only key is used for matching
  bool StillInUse(const Key& k) const { return StillInUse(k, nullptr); }
  bool StillInUse(const Key& k, const Value* value) const;

  // Remove any entry corresponding to "k" from the cache.  Note that
  // if the entry is pinned because of an earlier Lookup or
  // InsertPinned operation, the entry will disappear from further
  // Lookups, but will not actually be deleted until all of the pins
  // are released.
  //
  // "RemoveElement" will be called if an entry is actually removed.
  void Remove(const Key& k);

  // Removes all entries from the cache. The pinned entries will
  // disappear from further Lookups, but will not actually be deleted
  // until all of the pins are released. This is different from Clear()
  // because Clear() cleans up everything and requires that all Values are
  // unpinned.
  //
  // "Remove" will be called for each cache entry.
  void RemoveAll();

  // Remove all unpinned entries from the cache.
  // "RemoveElement" will be called for each such entry.
  void RemoveUnpinned();

  // Remove all entries from the cache.  It is an error to call this
  // operation if any entry in the cache is currently pinned.
  //
  // "RemoveElement" will be called for all removed entries.
  void Clear();

  // Remove all entries which have exceeded their max idle time or age
  // set using SetMaxIdleSeconds() or SetAgeBasedEviction() respectively.
  void RemoveExpiredEntries() {
    if (max_idle_ >= 0) DiscardIdle(max_idle_);
  }

  // Return current size of cache
  int64_t Size() const { return units_; }

  // Return number of entries in the cache.  This value may differ
  // from Size() if some of the elements have a cost != 1.
  int64_t Entries() const { return table_.size(); }

  // Return size of deferred deletions
  int64_t DeferredSize() const;

  // Return number of deferred deletions
  int64_t DeferredEntries() const;

  // Return size of entries that are pinned but not deferred
  int64_t PinnedSize() const { return pinned_units_; }

  // Return maximum size of cache
  int64_t MaxSize() const { return max_units_; }

  // Return the age (in microseconds) of the least recently used element in
  // the cache.  If the cache is empty, zero (0) is returned.
  int64_t AgeOfLRUItemInMicroseconds() const;

  // In LRU mode, this is the time of last use in cycles. Last use is defined
  // as time of last Release(), Insert() or InsertPinned() methods.
  //
  // The timer is not updated on Lookup(), so GetLastUseTime() will
  // still return time of previous access until Release().
  //
  // Returns -1 if key was not found, CycleClock cycle count otherwise.
  // REQUIRES: LRU mode
  int64_t GetLastUseTime(const Key& k) const;

  // For age-based mode, this is the time of element insertion in cycles,
  // set by Insert() and InsertPinned() methods.
  // Returns -1 if key was not found, CycleClock cycle count otherwise.
  // REQUIRES: age-based mode
  int64_t GetInsertionTime(const Key& k) const;

  // Invokes 'DebugIterator' on each element in the cache.  The
  // 'pin_count' argument supplied will be the pending reference count
  // for the element.  The 'is_deferred' argument will be true for
  // elements that have been removed but whose removal is deferred.
  // The supplied value for "ouput" will be passed to the DebugIterator.
  void DebugOutput(std::string* output) const;

  // Return a std::string that summarizes the contents of the cache.
  //
  // The output of this function is not suitable for parsing by borgmon. For an
  // example of exporting the summary information in a borgmon mapped-value
  // format, see GFS_CS_BufferCache::ExportSummaryAsMap in
  // file/gfs/chunkserver/gfs_chunkserver.{cc,h}
  std::string Summary() const {
    std::stringstream ss;
    ss << PinnedSize() << "/" << DeferredSize() << "/" << Size() << " p/d/a";
    return ss.str();
  }

  // STL style const_iterator support
  typedef SimpleLRUCacheConstIterator<Key, Value, MapType> const_iterator;
  friend class SimpleLRUCacheConstIterator<Key, Value, MapType>;
  const_iterator begin() const {
    return const_iterator(table_.begin(), table_.end());
  }
  const_iterator end() const {
    return const_iterator(table_.end(), table_.end());
  }

  // Invokes the 'resize' operation on the underlying map with the given
  // size hint.  The exact meaning of this operation and its availability
  // depends on the supplied MapType.
  void ResizeTable(typename MapType::size_type size_hint) {
    table_.resize(size_hint);
  }

 protected:
  // Override this operation if you want to control how a value is
  // cleaned up.  For example, if the value is a "File", you may want
  // to "Close" it instead of "delete"ing it.
  //
  // Not actually implemented here because often value's destructor is
  // protected, and the derived SimpleLRUCache is declared a friend,
  // so we implement it in the derived SimpleLRUCache.
  virtual void RemoveElement(const Key& k, Value* value) = 0;

  virtual void DebugIterator(const Key& k, const Value* value, int pin_count,
                             int64_t last_timestamp, bool is_deferred,
                             std::string* output) const {
    std::stringstream ss;
    ss << "ox" << std::hex << value << std::dec << ": pin: " << pin_count;
    ss << ", is_deferred: " << is_deferred;
    ss << ", last_use: " << last_timestamp << std::endl;
    *output += ss.str();
  }

  // Override this operation if you want to evict cache entries
  // based on parameters other than the total units stored.
  // For example, if the cache stores open sstables, where the cost
  // is the size in bytes of the open sstable, you may want to evict
  // entries from the cache not only before the max size in bytes
  // is reached but also before reaching the limit of open file
  // descriptors.  Thus, you may want to override this function in a
  // subclass and return true if either Size() is too large or
  // Entries() is too large.
  virtual bool IsOverfull() const { return units_ > max_units_; }

 private:
  typedef SimpleLRUCacheElem<Key, Value> Elem;
  typedef MapType Table;
  typedef typename Table::iterator TableIterator;
  typedef typename Table::const_iterator TableConstIterator;
  typedef MapType DeferredTable;
  typedef typename DeferredTable::iterator DeferredTableIterator;
  typedef typename DeferredTable::const_iterator DeferredTableConstIterator;

  Table table_;  // Main table
  // Pinned entries awaiting to be released before they can be discarded.
  // This is a key -> list mapping (multiple deferred entries for the same key)
  // The machinery used to maintain main LRU list is reused here, though this
  // list is not necessarily LRU and we don't care about the order of elements.
  DeferredTable defer_;
  int64_t units_;         // Combined units of all elements
  int64_t max_units_;     // Max allowed units
  int64_t pinned_units_;  // Combined units of all pinned elements
  Elem head_;             // Dummy head of LRU list (next is mru elem)
  int64_t max_idle_;      // Maximum number of idle cycles
  bool lru_;              // LRU or age-based eviction?

  // Representation invariants:
  // . LRU list is circular doubly-linked list
  // . Each live "Elem" is either in "table_" or "defer_"
  // . LRU list contains elements in "table_" that can be removed to free space
  // . Each "Elem" in "defer_" has a non-zero pin count

  void Discard(Elem* e) {
    assert(e->pin == 0);
    units_ -= e->units;
    RemoveElement(e->key, e->value);
    delete e;
  }

  // Count the number and total size of the elements in the deferred table.
  void CountDeferredEntries(int64_t* num_entries, int64_t* total_size) const;

  // Currently in deferred table?
  // Note, if (value == nullptr), only key is used for matching.
  bool InDeferredTable(const Key& k, const Value* value) const;

  void GarbageCollect();               // Discard to meet space constraints
  void DiscardIdle(int64_t max_idle);  // Discard to meet idle-time constraints

  void SetTimeout(double seconds, bool lru);

  bool IsOverfullInternal() const {
    return ((units_ > max_units_) || IsOverfull());
  }
  void Remove(Elem* e);

 public:
  static const size_t kElemSize = sizeof(Elem);

 private:
  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(SimpleLRUCacheBase);
};

template <class Key, class Value, class MapType, class EQ>
SimpleLRUCacheBase<Key, Value, MapType, EQ>::SimpleLRUCacheBase(
    int64_t total_units)
    : head_(Key(), nullptr, 0, 0, Elem::kNeverUsed) {
  units_ = 0;
  pinned_units_ = 0;
  max_units_ = total_units;
  head_.next = &head_;
  head_.prev = &head_;
  max_idle_ = -1;  // Stands for "no expiration"
  lru_ = true;     // default to LRU, not age-based
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::SetTimeout(double seconds,
                                                             bool lru) {
  if (seconds < 0 || std::isinf(seconds)) {
    // Treat as no expiration based on idle time
    lru_ = lru;
    max_idle_ = -1;
  } else if (max_idle_ >= 0 && lru != lru_) {
    // LOG(DFATAL) << "Can't SetMaxIdleSeconds() and SetAgeBasedEviction()";
    // In production we'll just ignore the second call
    assert(0);
  } else {
    lru_ = lru;

    // Convert to cycles ourselves in order to perform all calculations in
    // floating point so that we avoid integer overflow.
    // NOTE: The largest representable int64_t cannot be represented exactly as
    // a
    // double, so the cast results in a slightly larger value which cannot be
    // converted back to an int64_t. The next smallest double is representable
    // as
    // an int64_t, however, so if we make sure that `timeout_cycles` is strictly
    // smaller than the result of the cast, we know that casting
    // `timeout_cycles` to int64_t will not overflow.
    // NOTE 2: If you modify the computation here, make sure to update the
    // GetBoundaryTimeout() method in the test as well.
    const double timeout_cycles = seconds * SimpleCycleTimer::Frequency();
    if (timeout_cycles >= std::numeric_limits<int64_t>::max()) {
      // The value is outside the range of int64_t, so "round" down to something
      // that can be represented.
      max_idle_ = std::numeric_limits<int64_t>::max();
    } else {
      max_idle_ = static_cast<int64_t>(timeout_cycles);
    }
    DiscardIdle(max_idle_);
  }
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::RemoveAll() {
  // For each element: call "Remove"
  for (TableIterator iter = table_.begin(); iter != table_.end(); ++iter) {
    Remove(iter->second);
  }
  table_.clear();
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::RemoveUnpinned() {
  for (Elem* e = head_.next; e != &head_;) {
    Elem* next = e->next;
    if (e->pin == 0) Remove(e->key);
    e = next;
  }
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::Clear() {
  // For each element: call "RemoveElement" and delete it
  for (TableConstIterator iter = table_.begin(); iter != table_.end();) {
    Elem* e = iter->second;
    // Pre-increment the iterator to avoid possible
    // accesses to deleted memory in cases where the
    // key is a pointer to the memory that is freed by
    // Discard.
    ++iter;
    Discard(e);
  }
  // Pinned entries cannot be Discarded and defer_ contains nothing but pinned
  // entries. Therefore, it must be already be empty at this point.
  assert(defer_.empty());
  // Get back into pristine state
  table_.clear();
  head_.next = &head_;
  head_.prev = &head_;
  units_ = 0;
  pinned_units_ = 0;
}

template <class Key, class Value, class MapType, class EQ>
Value* SimpleLRUCacheBase<Key, Value, MapType, EQ>::LookupWithOptions(
    const Key& k, const SimpleLRUCacheOptions& options) {
  RemoveExpiredEntries();

  TableIterator iter = table_.find(k);
  if (iter != table_.end()) {
    // We set last_use_ upon Release, not during Lookup.
    Elem* e = iter->second;
    if (e->pin == 0) {
      pinned_units_ += e->units;
      // We are pinning this entry, take it off the LRU list if we are in LRU
      // mode. In strict age-based mode entries stay on the list while pinned.
      if (lru_ && options.update_eviction_order()) e->Unlink();
    }
    e->pin++;
    return e->value;
  }
  return nullptr;
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::ReleaseWithOptions(
    const Key& k, Value* value, const SimpleLRUCacheOptions& options) {
  {  // First check to see if this is a deferred value
    DeferredTableIterator iter = defer_.find(k);
    if (iter != defer_.end()) {
      const Elem* const head = iter->second;
      // Go from oldest to newest, assuming that oldest entries get released
      // first. This may or may not be true and makes no semantic difference.
      Elem* e = head->prev;
      while (e != head && e->value != value) {
        e = e->prev;
      }
      if (e->value == value) {
        // Found in deferred list: release it
        assert(e->pin > 0);
        e->pin--;
        if (e->pin == 0) {
          if (e == head) {
            // When changing the head, remove the head item and re-insert the
            // second item on the list (if there are any left). Do not re-use
            // the key from the first item.
            // Even though the two keys compare equal, the lifetimes may be
            // different (such as a key of Std::StringPiece).
            defer_.erase(iter);
            if (e->prev != e) {
              defer_[e->prev->key] = e->prev;
            }
          }
          e->Unlink();
          Discard(e);
        }
        return;
      }
    }
  }
  {  // Not deferred; so look in hash table
    TableIterator iter = table_.find(k);
    assert(iter != table_.end());
    Elem* e = iter->second;
    assert(e->value == value);
    assert(e->pin > 0);
    if (lru_ && options.update_eviction_order()) {
      e->last_use_ = SimpleCycleTimer::Now();
    }
    e->pin--;

    if (e->pin == 0) {
      if (lru_ && options.update_eviction_order()) e->Link(&head_);
      pinned_units_ -= e->units;
      if (IsOverfullInternal()) {
        // This element is no longer needed, and we are full.  So kick it out.
        Remove(k);
      }
    }
  }
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::InsertPinned(const Key& k,
                                                               Value* value,
                                                               size_t units) {
  // Get rid of older entry (if any) from table
  Remove(k);

  // Make new element
  Elem* e = new Elem(k, value, 1, units, SimpleCycleTimer::Now());

  // Adjust table, total units fields.
  units_ += units;
  pinned_units_ += units;
  table_[k] = e;

  // If we are in the strict age-based eviction mode, the entry goes on the LRU
  // list now and is never removed. In the LRU mode, the list will only contain
  // unpinned entries.
  if (!lru_) e->Link(&head_);
  GarbageCollect();
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::UpdateSize(const Key& k,
                                                             const Value* value,
                                                             size_t units) {
  TableIterator table_iter = table_.find(k);
  if ((table_iter != table_.end()) &&
      ((value == nullptr) || (value == table_iter->second->value))) {
    Elem* e = table_iter->second;
    units_ -= e->units;
    if (e->pin > 0) {
      pinned_units_ -= e->units;
    }
    e->units = units;
    units_ += e->units;
    if (e->pin > 0) {
      pinned_units_ += e->units;
    }
  } else {
    const DeferredTableIterator iter = defer_.find(k);
    if (iter != defer_.end()) {
      const Elem* const head = iter->second;
      Elem* e = iter->second;
      do {
        if (e->value == value || value == nullptr) {
          units_ -= e->units;
          e->units = units;
          units_ += e->units;
        }
        e = e->prev;
      } while (e != head);
    }
  }
  GarbageCollect();
}

template <class Key, class Value, class MapType, class EQ>
bool SimpleLRUCacheBase<Key, Value, MapType, EQ>::StillInUse(
    const Key& k, const Value* value) const {
  TableConstIterator iter = table_.find(k);
  if ((iter != table_.end()) &&
      ((value == nullptr) || (value == iter->second->value))) {
    return true;
  } else {
    return InDeferredTable(k, value);
  }
}

template <class Key, class Value, class MapType, class EQ>
bool SimpleLRUCacheBase<Key, Value, MapType, EQ>::InDeferredTable(
    const Key& k, const Value* value) const {
  const DeferredTableConstIterator iter = defer_.find(k);
  if (iter != defer_.end()) {
    const Elem* const head = iter->second;
    const Elem* e = head;
    do {
      if (e->value == value || value == nullptr) return true;
      e = e->prev;
    } while (e != head);
  }
  return false;
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::Remove(const Key& k) {
  TableIterator iter = table_.find(k);
  if (iter != table_.end()) {
    Elem* e = iter->second;
    table_.erase(iter);
    Remove(e);
  }
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::Remove(Elem* e) {
  // Unlink e whether it is in the LRU or the deferred list. It is safe to call
  // Unlink() if it is not in either list.
  e->Unlink();
  if (e->pin > 0) {
    pinned_units_ -= e->units;

    // Now add it to the deferred table.
    DeferredTableIterator iter = defer_.find(e->key);
    if (iter == defer_.end()) {
      // Inserting a new key, the element becomes the head of the list.
      e->prev = e->next = e;
      defer_[e->key] = e;
    } else {
      // There is already a deferred list for this key, attach the element to it
      Elem* head = iter->second;
      e->Link(head);
    }
  } else {
    Discard(e);
  }
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::GarbageCollect() {
  Elem* e = head_.prev;
  while (IsOverfullInternal() && (e != &head_)) {
    Elem* prev = e->prev;
    if (e->pin == 0) {
      // Erase from hash-table
      TableIterator iter = table_.find(e->key);
      assert(iter != table_.end());
      assert(iter->second == e);
      table_.erase(iter);
      e->Unlink();
      Discard(e);
    }
    e = prev;
  }
}

// Not using cycle. Instead using second from time()
static const int kAcceptableClockSynchronizationDriftCycles = 1;

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::DiscardIdle(
    int64_t max_idle) {
  if (max_idle < 0) return;

  Elem* e = head_.prev;
  const int64_t threshold = SimpleCycleTimer::Now() - max_idle;
#ifndef NDEBUG
  int64_t last = 0;
#endif
  while ((e != &head_) && (e->last_use_ < threshold)) {
// Sanity check: LRU list should be sorted by last_use_.  We could
// check the entire list, but that gives quadratic behavior.
//
// TSCs on different cores of multi-core machines sometime get slightly out
// of sync; compensate for this by allowing clock to go backwards by up to
// kAcceptableClockSynchronizationDriftCycles CPU cycles.
//
// A kernel bug (http://b/issue?id=777807) sometimes causes TSCs to become
// widely unsynchronized, in which case this CHECK will fail.  As a
// temporary work-around, running
//
//  $ sudo bash
//  # echo performance>/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor
//  # /etc/init.d/cpufrequtils restart
//
// fixes the problem.
#ifndef NDEBUG
    assert(last <= e->last_use_ + kAcceptableClockSynchronizationDriftCycles);
    last = e->last_use_;
#endif

    Elem* prev = e->prev;
    // There are no pinned elements on the list in the LRU mode, and in the
    // age-based mode we push them out of the main table regardless of pinning.
    assert(e->pin == 0 || !lru_);
    Remove(e->key);
    e = prev;
  }
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::CountDeferredEntries(
    int64_t* num_entries, int64_t* total_size) const {
  *num_entries = *total_size = 0;
  for (DeferredTableConstIterator iter = defer_.begin(); iter != defer_.end();
       ++iter) {
    const Elem* const head = iter->second;
    const Elem* e = head;
    do {
      (*num_entries)++;
      *total_size += e->units;
      e = e->prev;
    } while (e != head);
  }
}

template <class Key, class Value, class MapType, class EQ>
int64_t SimpleLRUCacheBase<Key, Value, MapType, EQ>::DeferredSize() const {
  int64_t entries, size;
  CountDeferredEntries(&entries, &size);
  return size;
}

template <class Key, class Value, class MapType, class EQ>
int64_t SimpleLRUCacheBase<Key, Value, MapType, EQ>::DeferredEntries() const {
  int64_t entries, size;
  CountDeferredEntries(&entries, &size);
  return entries;
}

template <class Key, class Value, class MapType, class EQ>
int64_t SimpleLRUCacheBase<Key, Value, MapType,
                           EQ>::AgeOfLRUItemInMicroseconds() const {
  if (head_.prev == &head_) return 0;
  return kSecToUsec * (SimpleCycleTimer::Now() - head_.prev->last_use_) /
         SimpleCycleTimer::Frequency();
}

template <class Key, class Value, class MapType, class EQ>
int64_t SimpleLRUCacheBase<Key, Value, MapType, EQ>::GetLastUseTime(
    const Key& k) const {
  // GetLastUseTime works only in LRU mode
  assert(lru_);
  TableConstIterator iter = table_.find(k);
  if (iter == table_.end()) return -1;
  const Elem* e = iter->second;
  return e->last_use_;
}

template <class Key, class Value, class MapType, class EQ>
int64_t SimpleLRUCacheBase<Key, Value, MapType, EQ>::GetInsertionTime(
    const Key& k) const {
  // GetInsertionTime works only in age-based mode
  assert(!lru_);
  TableConstIterator iter = table_.find(k);
  if (iter == table_.end()) return -1;
  const Elem* e = iter->second;
  return e->last_use_;
}

template <class Key, class Value, class MapType, class EQ>
void SimpleLRUCacheBase<Key, Value, MapType, EQ>::DebugOutput(
    std::string* output) const {
  std::stringstream ss;
  ss << "SimpleLRUCache of " << table_.size();
  ss << " elements plus " << DeferredEntries();
  ss << " deferred elements (" << Size();
  ss << " units, " << MaxSize() << " max units)";
  *output += ss.str();
  for (TableConstIterator iter = table_.begin(); iter != table_.end(); ++iter) {
    const Elem* e = iter->second;
    DebugIterator(e->key, e->value, e->pin, e->last_use_, false, output);
  }
  *output += "Deferred elements\n";
  for (DeferredTableConstIterator iter = defer_.begin(); iter != defer_.end();
       ++iter) {
    const Elem* const head = iter->second;
    const Elem* e = head;
    do {
      DebugIterator(e->key, e->value, e->pin, e->last_use_, true, output);
      e = e->prev;
    } while (e != head);
  }
}

// construct an iterator be sure to save a copy of end() as well, so we don't
// update external_view_ in that case. this is b/c if it_ == end(), calling
// it_->first segfaults.  we could do this by making sure a specific field in
// it_ is not nullptr but that relies on the internal implementation of it_, so
// we pass in end() instead
template <class MapType, class Key, class Value>
SimpleLRUCacheConstIterator<MapType, Key, Value>::SimpleLRUCacheConstIterator(
    HashMapConstIterator it, HashMapConstIterator end)
    : it_(it), end_(end) {
  if (it_ != end_) {
    external_view_.first = it_->first;
    external_view_.second = it_->second->value;
    last_use_ = it_->second->last_use_;
  }
}

template <class MapType, class Key, class Value>
auto SimpleLRUCacheConstIterator<MapType, Key, Value>::operator++()
    -> SimpleLRUCacheConstIterator& {
  it_++;
  if (it_ != end_) {
    external_view_.first = it_->first;
    external_view_.second = it_->second->value;
    last_use_ = it_->second->last_use_;
  }
  return *this;
}

template <class Key, class Value, class H, class EQ>
class SimpleLRUCache
    : public SimpleLRUCacheBase<
          Key, Value,
          std::unordered_map<Key, SimpleLRUCacheElem<Key, Value>*, H, EQ>, EQ> {
 public:
  explicit SimpleLRUCache(int64_t total_units)
      : SimpleLRUCacheBase<
            Key, Value,
            std::unordered_map<Key, SimpleLRUCacheElem<Key, Value>*, H, EQ>,
            EQ>(total_units) {}

 protected:
  virtual void RemoveElement(const Key& k, Value* value) { delete value; }

 private:
  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(SimpleLRUCache);
};

template <class Key, class Value, class Deleter, class H, class EQ>
class SimpleLRUCacheWithDeleter
    : public SimpleLRUCacheBase<
          Key, Value,
          std::unordered_map<Key, SimpleLRUCacheElem<Key, Value>*, H, EQ>, EQ> {
  typedef std::unordered_map<Key, SimpleLRUCacheElem<Key, Value>*, H, EQ>
      HashMap;
  typedef SimpleLRUCacheBase<Key, Value, HashMap, EQ> Base;

 public:
  explicit SimpleLRUCacheWithDeleter(int64_t total_units)
      : Base(total_units), deleter_() {}

  SimpleLRUCacheWithDeleter(int64_t total_units, Deleter deleter)
      : Base(total_units), deleter_(deleter) {}

 protected:
  virtual void RemoveElement(const Key& k, Value* value) { deleter_(value); }

 private:
  Deleter deleter_;
  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(SimpleLRUCacheWithDeleter);
};

}  // namespace utils
}  // namespace istio

#endif  // ISTIO_UTILS_SIMPLE_LRU_CACHE_INL_H_
