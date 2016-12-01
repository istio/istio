// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/path_matcher_node.h"
#include "src/api_manager/http_template.h"

#include <utility>
#include <vector>

using std::string;
using std::vector;

namespace google {
namespace api_manager {

const char HttpMethod_WILD_CARD[] = "*";

namespace {
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
PathMatcherNode::PathInfo::Builder::AppendLiteralNode(string name) {
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

// TODO: When possible, replace the children_ map's plain pointers with
// smart pointers. (small_map and hash_map are not move-aware, so they cannot
// hold unique_ptrs)
PathMatcherNode::~PathMatcherNode() { utils::STLDeleteValues(&children_); }

// Performs a deep copy of the parameter |node|. NB: WrapperGraph::SharedPtr is
// a shared_ptr, so wrapper_graph_map_ copies point to the same WrapperGraph.
// TODO: Consider returning by value. Since copying is not allowed and it
// would not be possible to heap-allocate a cloned node, would it still be
// possible to store children in an associative container?
PathMatcherNode* PathMatcherNode::Clone() const {
  PathMatcherNode* clone = new PathMatcherNode();
  clone->result_map_ = result_map_;
  // deep-copy literal children
  for (const auto& entry : children_) {
    clone->children_[entry.first] = entry.second->Clone();
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
        const PathMatcherNode* child = pair->second;
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

  for (const string& child_key :
       {HttpTemplate::kSingleParameterKey, HttpTemplate::kWildCardPathPartKey,
        HttpTemplate::kWildCardPathKey}) {
    if (LookupPathFromChild(child_key, current, end, http_method, result)) {
      return;
    }
  }
  return;
}

bool PathMatcherNode::InsertPath(const PathInfo& node_path_info,
                                 string http_method, string service_name,
                                 void* method_data, bool mark_duplicates) {
  return this->InsertTemplate(node_path_info.path_info().begin(),
                              node_path_info.path_info().end(), http_method,
                              service_name, method_data, mark_duplicates);
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
    const vector<string>::const_iterator current,
    const vector<string>::const_iterator end, HttpMethod http_method,
    string service_name, void* method_data, bool mark_duplicates) {
  if (current == end) {
    PathMatcherLookupResult* const existing = utils::InsertOrReturnExisting(
        &result_map_, http_method, PathMatcherLookupResult(method_data, false));
    if (existing != nullptr) {
      existing->data = method_data;
      if (mark_duplicates) {
#if 0
        LOG(WARNING) << "The HTTP path '" << http_method << ":"
                     << ConvertHttpRuleToString(*wrapper_graph->http_rule())
                     << "' has already been registered to the host '"
                     << service_name << "'.";
#endif
        existing->is_multiple = true;
      }
      return false;
    }
    return true;
  }
  PathMatcherNode* child = utils::LookupOrInsertNew(&children_, *current);
  if (*current == HttpTemplate::kWildCardPathKey) {
    child->set_wildcard(true);
  }
  return child->InsertTemplate(current + 1, end, http_method, service_name,
                               method_data, mark_duplicates);
}

bool PathMatcherNode::LookupPathFromChild(
    const string child_key, const RequestPathParts::const_iterator current,
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

}  // namespace api_manager
}  // namespace google
