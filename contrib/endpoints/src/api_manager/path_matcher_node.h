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
#ifndef API_MANAGER_PATH_MATCHER_NODE_H_
#define API_MANAGER_PATH_MATCHER_NODE_H_

#include <map>
#include <memory>
#include <string>
#include <unordered_map>
#include <vector>

#include "src/api_manager/utils/stl_util.h"

namespace google {
namespace api_manager {

typedef std::string HttpMethod;

struct PathMatcherLookupResult {
  PathMatcherLookupResult() : data(nullptr), is_multiple(false) {}

  PathMatcherLookupResult(void* data, bool is_multiple)
      : data(data), is_multiple(is_multiple) {}

  // The WrapperGraph that is registered to a method (or HTTP path).
  void* data;
  // Whether the method (or path) has been registered for more than once.
  bool is_multiple;
};

// PathMatcherNodes represents a path part in a PathMatcher trie. Children nodes
// represent adjacent path parts. A node can have many literal children, one
// single-parameter child, and one repeated-parameter child.
//
// Thread Compatible.
class PathMatcherNode {
 public:
  // Provides information for inserting templates into the trie. Clients can
  // instantiate PathInfo with the provided Builder.
  class PathInfo {
   public:
    class Builder {
     public:
      friend class PathInfo;
      Builder() : path_() {}
      ~Builder() {}

      PathMatcherNode::PathInfo Build() const;

      // Appends a node that must match the string value of a request part. The
      // strings "/." and "/.." are disallowed.
      //
      // Example:
      //
      // builder.AppendLiteralNode("a")
      //        .AppendLiteralNode("b")
      //        .AppendLiteralNode("c");
      //
      // Matches the request path: a/b/c
      Builder& AppendLiteralNode(std::string name);

      // Appends a node that ignores the string value and matches any single
      // request part.
      //
      // Example:
      //
      // builder.AppendLiteralNode("a")
      //        .AppendSingleParameterNode()
      //        .AppendLiteralNode("c");
      //
      // Matching request paths: a/foo/c, a/bar/c, a/1/c
      Builder& AppendSingleParameterNode();

      // TODO: Appends a node that ignores string values and matches any
      // number of consecutive request parts.
      //
      // Example:
      //
      // builder.AppendLiteralNode("a")
      //        .AppendLiteralNode("b")
      //        .AppendRepeatedParameterNode();
      //
      // Matching request paths: a/b/1/2/3/4/5, a/b/c
      // Builder& AppendRepeatedParameterNode();

     private:
      std::vector<std::string> path_;
    };  // class Builder

    ~PathInfo() {}

    // Returns path information used to insert a new path into a PathMatcherNode
    // trie.
    const std::vector<std::string>& path_info() const { return path_; }

   private:
    explicit PathInfo(const Builder& builder) : path_(builder.path_) {}
    std::vector<std::string> path_;
  };  // class PathInfo

  typedef std::vector<std::string> RequestPathParts;

  // Creates a Root node with an empty WrapperGraph map.
  PathMatcherNode() : result_map_(), children_(), wildcard_(false) {}

  ~PathMatcherNode();

  // Creates a clone of this node and its subtrie
  PathMatcherNode* Clone() const;

  // Searches subtrie by finding a matching child for the current path part. If
  // a matching child exists, this function recurses on current + 1 with that
  // child as the receiver. If a matching descendant is found for the last part
  // in then this method copies the matching descendant's WrapperGraph,
  // VariableBindingInfoMap to the result pointers.
  void LookupPath(const RequestPathParts::const_iterator current,
                  const RequestPathParts::const_iterator end,
                  HttpMethod http_method,
                  PathMatcherLookupResult* result) const;

  // This method inserts a path of nodes into this subtrie. The WrapperGraph,
  // VariableBindingInfoMap are inserted at the terminal descendant node.
  // Returns true if the template didn't previously exist. Returns false
  // otherwise and depends on if mark_duplicates is true, the template will be
  // marked as having been registered for more than once and the lookup of the
  // template will yield a special error reporting WrapperGraph.
  bool InsertPath(const PathInfo& node_path_info, std::string http_method,
                  std::string service_name, void* method_data,
                  bool mark_duplicates);

  void set_wildcard(bool wildcard) { wildcard_ = wildcard; }

 private:
  // This method inserts a path of nodes into this subtrie (described by the
  // vector<Info>, starting from the |current| position in the iterator of path
  // parts, and if necessary, creating intermediate nodes along the way. The
  // WrapperGraph, VariableBindingInfoMap are inserted at the terminal
  // descendant node (which corresponds to the string part in the iterator).
  // Returns true if the template didn't previously exist. Returns false
  // otherwise and depends on if mark_duplicates is true, the template will be
  // marked as having been registered for more than once and the lookup of the
  // template will yield a special error reporting WrapperGraph.
  bool InsertTemplate(const std::vector<std::string>::const_iterator current,
                      const std::vector<std::string>::const_iterator end,
                      HttpMethod http_method, std::string service_name,
                      void* method_data, bool mark_duplicates);

  // Helper method for LookupPath. If the given child key exists, search
  // continues on the child node pointed by the child key with the next part
  // in the path. Returns true if found a match for the path eventually.
  bool LookupPathFromChild(const std::string child_key,
                           const RequestPathParts::const_iterator current,
                           const RequestPathParts::const_iterator end,
                           HttpMethod http_method,
                           PathMatcherLookupResult* result) const;

  // If a WrapperGraph is found for the provided key, then this method returns
  // true and copies the WrapperGraph to the provided result pointer. If no
  // match is found, this method returns false and leaves the result unmodified.
  //
  // NB: If result == nullptr, method will return bool value without modifying
  // result.
  bool GetResultForHttpMethod(HttpMethod key,
                              PathMatcherLookupResult* result) const;

  std::map<HttpMethod, PathMatcherLookupResult> result_map_;

  // Lookup must be FAST
  //
  // n: the number of paths registered per client varies, but we can expect the
  // size of |children_| to range from ~5 to ~100 entries.
  //
  // To ensure fast lookups when n grows large, it is prudent to consider an
  // alternative to binary search on a sorted vector.
  //
  // hash_map provides O(1) expected lookups for all n, but has undesirable
  // memory overhead when n is small. To address this overhead, we use a
  // small_map hybrid that provides O(1) expected lookups with a small memory
  // footprint. The small_map scales up to a hash_map when the number of literal
  // children grows large.
  std::unordered_map<std::string, PathMatcherNode*> children_;

  // True if this node represents a wildcard path '**'.
  bool wildcard_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_PATH_MATCHER_NODE_H_
