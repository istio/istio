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

#ifndef API_SPEC_PATH_MATCHER_NODE_H_
#define API_SPEC_PATH_MATCHER_NODE_H_

#include <map>
#include <memory>
#include <string>
#include <unordered_map>
#include <vector>

namespace istio {
namespace api_spec {

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
  std::unique_ptr<PathMatcherNode> Clone() const;

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
                  void* method_data, bool mark_duplicates);

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
                      HttpMethod http_method, void* method_data,
                      bool mark_duplicates);

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
  std::unordered_map<std::string, std::unique_ptr<PathMatcherNode>> children_;

  // True if this node represents a wildcard path '**'.
  bool wildcard_;
};

}  // namespace api_spec
}  // namespace istio

#endif  // API_SPEC_PATH_MATCHER_NODE_H_
