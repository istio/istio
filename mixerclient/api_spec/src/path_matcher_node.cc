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

#include "path_matcher_node.h"
#include "http_template.h"

namespace istio {
namespace api_spec {

const char HttpMethod_WILD_CARD[] = "*";

namespace {

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

// Returns a reference to the pointer associated with key. If not found,
// a pointee is constructed and added to the map. In that case, the new
// pointee is value-initialized (aka "default-constructed").
// Useful for containers of the form Map<Key, Ptr>, where Ptr is pointer-like.
template <class Collection>
typename Collection::value_type::second_type& LookupOrInsertNew(
    Collection* const collection,
    const typename Collection::value_type::first_type& key) {
  typedef typename Collection::value_type::second_type Mapped;
  typedef typename Mapped::element_type Element;
  std::pair<typename Collection::iterator, bool> ret =
      collection->insert(typename Collection::value_type(key, Mapped()));
  if (ret.second) {
    ret.first->second = Mapped(new Element());
  }
  return ret.first->second;
}

// A convinent function to lookup a STL colllection with two keys.
// Lookup key1 first, if not found, lookup key2, or return nullptr.
template <class Collection>
const typename Collection::value_type::second_type* Find2KeysOrNull(
    const Collection& collection,
    const typename Collection::value_type::first_type& key1,
    const typename Collection::value_type::first_type& key2) {
  auto it = collection.find(key1);
  if (it == collection.end()) {
    it = collection.find(key2);
    if (it == collection.end()) {
      return nullptr;
    }
  }
  return &it->second;
}
}  // namespace

PathMatcherNode::PathInfo::Builder&
PathMatcherNode::PathInfo::Builder::AppendLiteralNode(std::string name) {
  if (name == HttpTemplate::kSingleParameterKey) {
    //    status_.Update(util::Status(util::error::INVALID_ARGUMENT,
    //                          StrCat(name, " is a reserved node name.")));
  }
  path_.emplace_back(name);
  return *this;
}

PathMatcherNode::PathInfo::Builder&
PathMatcherNode::PathInfo::Builder::AppendSingleParameterNode() {
  path_.emplace_back(HttpTemplate::kSingleParameterKey);
  return *this;
}

PathMatcherNode::PathInfo PathMatcherNode::PathInfo::Builder::Build() const {
  return PathMatcherNode::PathInfo(*this);
}

PathMatcherNode::~PathMatcherNode() {}

std::unique_ptr<PathMatcherNode> PathMatcherNode::Clone() const {
  std::unique_ptr<PathMatcherNode> clone(new PathMatcherNode());
  clone->result_map_ = result_map_;
  // deep-copy literal children
  for (const auto& entry : children_) {
    clone->children_.emplace(entry.first, entry.second->Clone());
  }
  clone->wildcard_ = wildcard_;
  return clone;
}

// This recursive function performs an exhaustive DFS of the node's subtrie.
// The node attempts to find a match for the current part of the path among its
// children. Children are considered in sequence according to Google HTTP
// Template Spec matching precedence. If a match is found, the method recurses
// on the matching child with the next part in path.
//
// NB: If this path segment is of repeated-variable type and no matching child
// is found, the receiver recurses on itself with the next path part.
//
// Base Case: |current| is beyond the range of the path parts
// ==========
// The receiver node matched the final part in |path|. If a WrapperGraph exists
// for the given HTTP method, the method copies to the node's WrapperGraph to
// result and returns true.
void PathMatcherNode::LookupPath(const RequestPathParts::const_iterator current,
                                 const RequestPathParts::const_iterator end,
                                 HttpMethod http_method,
                                 PathMatcherLookupResult* result) const {
  // base case
  if (current == end) {
    if (!GetResultForHttpMethod(http_method, result)) {
      // If we didn't find a wrapper graph at this node, check if we have one
      // in a wildcard (**) child. If we do, use it. This will ensure we match
      // the root with wildcard templates.
      auto pair = children_.find(HttpTemplate::kWildCardPathKey);
      if (pair != children_.end()) {
        const auto& child = pair->second;
        child->GetResultForHttpMethod(http_method, result);
      }
    }
    return;
  }
  if (LookupPathFromChild(*current, current, end, http_method, result)) {
    return;
  }
  // For wild card node, keeps searching for next path segment until either
  // 1) reaching the end (/foo/** case), or 2) all remaining segments match
  // one of child branches (/foo/**/bar/xyz case).
  if (wildcard_) {
    LookupPath(current + 1, end, http_method, result);
    // Since only constant segments are allowed after wild card, no need to
    // search another wild card nodes from children, so bail out here.
    return;
  }

  for (const std::string& child_key :
       {HttpTemplate::kSingleParameterKey, HttpTemplate::kWildCardPathPartKey,
        HttpTemplate::kWildCardPathKey}) {
    if (LookupPathFromChild(child_key, current, end, http_method, result)) {
      return;
    }
  }
  return;
}

bool PathMatcherNode::InsertPath(const PathInfo& node_path_info,
                                 std::string http_method, void* method_data,
                                 bool mark_duplicates) {
  return InsertTemplate(node_path_info.path_info().begin(),
                        node_path_info.path_info().end(), http_method,
                        method_data, mark_duplicates);
}

// This method locates a matching child for the |current| path part, inserting a
// child if not present. Then, the method recurses on this matching child with
// the next template path part.
//
// Base Case: |current| is beyond the range of the path parts
// ==========
// This node matched the final part in the iterator of parts. This method
// updates the node's WrapperGraph for the specified HTTP method.
bool PathMatcherNode::InsertTemplate(
    const std::vector<std::string>::const_iterator current,
    const std::vector<std::string>::const_iterator end, HttpMethod http_method,
    void* method_data, bool mark_duplicates) {
  if (current == end) {
    PathMatcherLookupResult* const existing = InsertOrReturnExisting(
        &result_map_, http_method, PathMatcherLookupResult(method_data, false));
    if (existing != nullptr) {
      if (mark_duplicates) {
        existing->is_multiple = true;
      }
      return false;
    }
    return true;
  }
  std::unique_ptr<PathMatcherNode>& child =
      LookupOrInsertNew(&children_, *current);
  if (*current == HttpTemplate::kWildCardPathKey) {
    child->set_wildcard(true);
  }
  return child->InsertTemplate(current + 1, end, http_method, method_data,
                               mark_duplicates);
}

bool PathMatcherNode::LookupPathFromChild(
    const std::string child_key, const RequestPathParts::const_iterator current,
    const RequestPathParts::const_iterator end, HttpMethod http_method,
    PathMatcherLookupResult* result) const {
  auto pair = children_.find(child_key);
  if (pair != children_.end()) {
    pair->second->LookupPath(current + 1, end, http_method, result);
    if (result != nullptr && result->data != nullptr) {
      return true;
    }
  }
  return false;
}

bool PathMatcherNode::GetResultForHttpMethod(
    HttpMethod key, PathMatcherLookupResult* result) const {
  const PathMatcherLookupResult* found_p =
      Find2KeysOrNull(result_map_, key, HttpMethod_WILD_CARD);
  if (found_p != nullptr) {
    *result = *found_p;
    return true;
  }
  return false;
}

}  // namespace api_spec
}  // namespace istio
