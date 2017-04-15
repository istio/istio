/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_PATH_MATCHER_H_
#define API_MANAGER_PATH_MATCHER_H_

#include <stddef.h>
#include <memory>
#include <set>
#include <string>
#include <unordered_map>

#include "contrib/endpoints/include/api_manager/method.h"
#include "contrib/endpoints/include/api_manager/method_call_info.h"
#include "contrib/endpoints/src/api_manager/http_template.h"
#include "contrib/endpoints/src/api_manager/path_matcher_node.h"

namespace google {
namespace api_manager {

class PathMatcherBuilder;  // required for PathMatcher constructor

// The immutable, thread safe PathMatcher stores a mapping from a combination of
// a service (host) name and a HTTP path to your method (MethodInfo*). It is
// constructed with a PathMatcherBuilder and supports one operation: Lookup.
// Clients may use this method to locate your method (MethodInfo*) for a
// combination of service name and HTTP URL path.
//
// Usage example:
// 1) building the PathMatcher:
//     PathMatcherBuilder builder(false);
//     for each (service_name, http_method, url_path, associated method)
//         builder.register(service_name, http_method, url_path, datat);
//     PathMater matcher = builder.Build();
// 2) lookup:
//      MethodInfo * method = matcher.Lookup(service_name, http_method,
//                                           url_path);
//      if (method == nullptr)  failed to find it.
//
class PathMatcher {
 public:
  ~PathMatcher(){};

  MethodInfo *Lookup(const std::string &http_method, const std::string &path,
                     const std::string &query_params,
                     std::vector<VariableBinding> *variable_bindings,
                     std::string *body_field_path) const;

  MethodInfo *Lookup(const std::string &http_method,
                     const std::string &path) const;

 private:
  // Creates a Path Matcher with a Builder by moving the builder's root node.
  explicit PathMatcher(PathMatcherBuilder &&builder);

  // A root node shared by all services, i.e. paths of all services will be
  // registered to this node.
  std::unique_ptr<PathMatcherNode> root_ptr_;
  // Holds the set of custom verbs found in configured templates.
  std::set<std::string> custom_verbs_;
  // Data we store per each registered method
  struct MethodData {
    MethodInfo *method;
    std::vector<HttpTemplate::Variable> variables;
    std::string body_field_path;
  };
  // The info associated with each method. The path matcher nodes
  // will hold pointers to MethodData objects in this vector.
  std::vector<std::unique_ptr<MethodData>> methods_;

 private:
  friend class PathMatcherBuilder;
};

typedef std::unique_ptr<PathMatcher> PathMatcherPtr;

// This PathMatcherBuilder is used to register path-WrapperGraph pairs and
// instantiate an immutable, thread safe PathMatcher.
//
// The PathMatcherBuilder itself is NOT THREAD SAFE.
class PathMatcherBuilder {
 public:
  PathMatcherBuilder();
  ~PathMatcherBuilder() {}

  // Registers a method.
  //
  // Registrations are one-to-one. If this function is called more than once, it
  // replaces the existing method. Only the last registered method is stored.
  // Return false if path is an invalid http template.
  bool Register(std::string http_method, std::string path,
                std::string body_field_path, MethodInfo *method);

  // Returns a unique_ptr to a thread safe PathMatcher that contains all
  // registered path-WrapperGraph pairs. Note the PathMatchBuilder instance
  // will be moved so cannot use after invoking Build().
  PathMatcherPtr Build();

 private:
  // Inserts a path to a PathMatcherNode.
  void InsertPathToNode(const PathMatcherNode::PathInfo &path,
                        void *method_data, std::string http_method,
                        bool mark_duplicates, PathMatcherNode *root_ptr);
  // A root node shared by all services, i.e. paths of all services will be
  // registered to this node.
  std::unique_ptr<PathMatcherNode> root_ptr_;
  // The set of custom verbs configured.
  // TODO: Perhaps this should not be at this level because there will
  // be multiple templates in different services on a server. Consider moving
  // this to PathMatcherNode.
  std::set<std::string> custom_verbs_;
  typedef PathMatcher::MethodData MethodData;
  std::vector<std::unique_ptr<MethodData>> methods_;

  friend class PathMatcher;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_PATH_MATCHER_H_
