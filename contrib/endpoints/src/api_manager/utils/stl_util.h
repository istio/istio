/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */
#ifndef API_MANAGER_UTILS_STL_UTIL_H_
#define API_MANAGER_UTILS_STL_UTIL_H_

namespace util {
namespace gtl {
namespace map_util_internal {

// Local implementation of RemoveConst to avoid including base/type_traits.h.
template <class T>
struct RemoveConst {
  typedef T type;
};
template <class T>
struct RemoveConst<const T> : RemoveConst<T> {};

// PointeeType<P>::type is the pointee of smart or raw pointer P.
template <typename P>
struct PointeeTypeImpl {
  typedef typename P::element_type type;
};
template <typename T>
struct PointeeTypeImpl<T*> {
  typedef T type;
};
template <typename P>
struct PointeeType : PointeeTypeImpl<P> {};
template <typename P>
struct PointeeType<const P> : PointeeType<P> {};
template <typename P>
struct PointeeType<P&> : PointeeType<P> {};

}  // namespace map_util_internal
}  // namespace gtl
}  // namespace util

namespace google {
namespace api_manager {
namespace utils {

// Calls delete (non-array version) on pointers in the range [begin, end).
//
// Note: If you're calling this on an entire container, you probably want to
// call STLDeleteElements(&container) instead (which also clears the container),
// or use an ElementDeleter.
template <typename ForwardIterator>
void STLDeleteContainerPointers(ForwardIterator begin, ForwardIterator end) {
  while (begin != end) {
    ForwardIterator temp = begin;
    ++begin;
    delete *temp;
  }
}

// Calls delete (non-array version) on BOTH items (pointers) in each pair in the
// range [begin, end).
template <typename ForwardIterator>
void STLDeleteContainerPairPointers(ForwardIterator begin,
                                    ForwardIterator end) {
  while (begin != end) {
    ForwardIterator temp = begin;
    ++begin;
    delete temp->first;
    delete temp->second;
  }
}

// Calls delete (non-array version) on the FIRST item (pointer) in each pair in
// the range [begin, end).
template <typename ForwardIterator>
void STLDeleteContainerPairFirstPointers(ForwardIterator begin,
                                         ForwardIterator end) {
  while (begin != end) {
    ForwardIterator temp = begin;
    ++begin;
    delete temp->first;
  }
}

// Calls delete (non-array version) on the SECOND item (pointer) in each pair in
// the range [begin, end).
//
// Note: If you're calling this on an entire container, you probably want to
// call STLDeleteValues(&container) instead, or use ValueDeleter.
template <typename ForwardIterator>
void STLDeleteContainerPairSecondPointers(ForwardIterator begin,
                                          ForwardIterator end) {
  while (begin != end) {
    ForwardIterator temp = begin;
    ++begin;
    delete temp->second;
  }
}

// Deletes all the elements in an STL container and clears the container. This
// function is suitable for use with a vector, set, hash_set, or any other STL
// container which defines sensible begin(), end(), and clear() methods.
//
// If container is nullptr, this function is a no-op.
//
// As an alternative to calling STLDeleteElements() directly, consider
// ElementDeleter (defined below), which ensures that your container's elements
// are deleted when the ElementDeleter goes out of scope.
template <typename T>
void STLDeleteElements(T* container) {
  if (!container) return;
  STLDeleteContainerPointers(container->begin(), container->end());
  container->clear();
}

// Given an STL container consisting of (key, value) pairs, STLDeleteValues
// deletes all the "value" components and clears the container. Does nothing in
// the case it's given a nullptr.
template <typename T>
void STLDeleteValues(T* v) {
  if (!v) return;
  STLDeleteContainerPairSecondPointers(v->begin(), v->end());
  v->clear();
}

// Returns a reference to the pointer associated with key. If not found,
// a pointee is constructed and added to the map. In that case, the new
// pointee is value-initialized (aka "default-constructed").
// Useful for containers of the form Map<Key, Ptr>, where Ptr is pointer-like.
template <class Collection>
typename Collection::value_type::second_type& LookupOrInsertNew(
    Collection* const collection,
    const typename Collection::value_type::first_type& key) {
  typedef typename Collection::value_type::second_type Mapped;
  typedef
      typename util::gtl::map_util_internal::PointeeType<Mapped>::type Element;
  std::pair<typename Collection::iterator, bool> ret =
      collection->insert(typename Collection::value_type(key, Mapped()));
  if (ret.second) {
    ret.first->second = Mapped(new Element());
  }
  return ret.first->second;
}

// A variant that accepts and forwards a pointee constructor argument.
template <class Collection, class Arg>
typename Collection::value_type::second_type& LookupOrInsertNew(
    Collection* const collection,
    const typename Collection::value_type::first_type& key, const Arg& arg) {
  typedef typename Collection::value_type::second_type Mapped;
  typedef
      typename util::gtl::map_util_internal::PointeeType<Mapped>::type Element;
  std::pair<typename Collection::iterator, bool> ret =
      collection->insert(typename Collection::value_type(key, Mapped()));
  if (ret.second) {
    ret.first->second = Mapped(new Element(arg));
  }
  return ret.first->second;
}

// Inserts the given key and value into the given collection if and only if the
// given key did NOT already exist in the collection. If the key previously
// existed in the collection, the value is not changed. Returns true if the
// key-value pair was inserted; returns false if the key was already present.
template <class Collection>
bool InsertIfNotPresent(Collection* const collection,
                        const typename Collection::value_type& vt) {
  return collection->insert(vt).second;
}

// Same as above except the key and value are passed separately.
template <class Collection>
bool InsertIfNotPresent(
    Collection* const collection,
    const typename Collection::value_type::first_type& key,
    const typename Collection::value_type::second_type& value) {
  return InsertIfNotPresent(collection,
                            typename Collection::value_type(key, value));
}

// Inserts the given key-value pair into the collection. Returns true if and
// only if the key from the given pair didn't previously exist. Otherwise, the
// value in the map is replaced with the value from the given pair.
template <class Collection>
bool InsertOrUpdate(Collection* const collection,
                    const typename Collection::value_type& vt) {
  std::pair<typename Collection::iterator, bool> ret = collection->insert(vt);
  if (!ret.second) {
    // update
    ret.first->second = vt.second;
    return false;
  }
  return true;
}

// Same as above, except that the key and value are passed separately.
template <class Collection>
bool InsertOrUpdate(Collection* const collection,
                    const typename Collection::value_type::first_type& key,
                    const typename Collection::value_type::second_type& value) {
  return InsertOrUpdate(collection,
                        typename Collection::value_type(key, value));
}

// Tries to insert the given key-value pair into the collection. Returns nullptr
// if the insert succeeds. Otherwise, returns a pointer to the existing value.
//
// This complements UpdateReturnCopy in that it allows to update only after
// verifying the old value and still insert quickly without having to look up
// twice. Unlike UpdateReturnCopy this also does not come with the issue of an
// undefined previous* in case new data was inserted.
template <class Collection>
typename Collection::value_type::second_type* InsertOrReturnExisting(
    Collection* const collection, const typename Collection::value_type& vt) {
  std::pair<typename Collection::iterator, bool> ret = collection->insert(vt);
  if (ret.second) {
    return nullptr;  // Inserted, no existing previous value.
  } else {
    return &ret.first->second;  // Return address of already existing value.
  }
}

// Same as above, except for explicit key and data.
template <class Collection>
typename Collection::value_type::second_type* InsertOrReturnExisting(
    Collection* const collection,
    const typename Collection::value_type::first_type& key,
    const typename Collection::value_type::second_type& data) {
  return InsertOrReturnExisting(collection,
                                typename Collection::value_type(key, data));
}

// Returns a const reference to the value associated with the given key if it
// exists, otherwise returns a const reference to the provided default value.
//
// WARNING: If a temporary object is passed as the default "value,"
// this function will return a reference to that temporary object,
// which will be destroyed at the end of the statement. A common
// example: if you have a map with string values, and you pass a char*
// as the default "value," either use the returned value immediately
// or store it in a string (not string&).
template <class Collection>
const typename Collection::value_type::second_type& FindWithDefault(
    const Collection& collection,
    const typename Collection::value_type::first_type& key,
    const typename Collection::value_type::second_type& value) {
  typename Collection::const_iterator it = collection.find(key);
  if (it == collection.end()) {
    return value;
  }
  return it->second;
}

// Returns a pointer to the const value associated with the given key if it
// exists, or nullptr otherwise.
template <class Collection>
const typename Collection::value_type::second_type* FindOrNull(
    const Collection& collection,
    const typename Collection::value_type::first_type& key) {
  typename Collection::const_iterator it = collection.find(key);
  if (it == collection.end()) {
    return 0;
  }
  return &it->second;
}

// Same as above but returns a pointer to the non-const value.
template <class Collection>
typename Collection::value_type::second_type* FindOrNull(
    Collection& collection,  // NOLINT
    const typename Collection::value_type::first_type& key) {
  typename Collection::iterator it = collection.find(key);
  if (it == collection.end()) {
    return 0;
  }
  return &it->second;
}

// Returns the pointer value associated with the given key. If none is found,
// nullptr is returned. The function is designed to be used with a map of keys
// to pointers.
//
// This function does not distinguish between a missing key and a key mapped
// to a nullptr value.
template <class Collection>
typename Collection::value_type::second_type FindPtrOrNull(
    const Collection& collection,
    const typename Collection::value_type::first_type& key) {
  typename Collection::const_iterator it = collection.find(key);
  if (it == collection.end()) {
    return typename Collection::value_type::second_type();
  }
  return it->second;
}

// Same as above, except takes non-const reference to collection.
//
// This function is needed for containers that propagate constness to the
// pointee, such as boost::ptr_map.
template <class Collection>
typename Collection::value_type::second_type FindPtrOrNull(
    Collection& collection,  // NOLINT
    const typename Collection::value_type::first_type& key) {
  typename Collection::iterator it = collection.find(key);
  if (it == collection.end()) {
    return typename Collection::value_type::second_type();
  }
  return it->second;
}

}  // namespace utils
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_UTILS_STL_UTIL_H_
