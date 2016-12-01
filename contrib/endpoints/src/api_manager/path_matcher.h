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
#ifndef API_MANAGER_PATH_MATCHER_H_
#define API_MANAGER_PATH_MATCHER_H_

#include <stddef.h>
#include <memory>
#include <set>
#include <string>
#include <unordered_map>

#include "include/api_manager/method.h"
#include "include/api_manager/method_call_info.h"
#include "src/api_manager/http_template.h"
#include "src/api_manager/path_matcher_node.h"

namespace google {
namespace api_manager {

class PathMatcher;         // required for typedef PathMatcherPtr
class PathMatcherBuilder;  // required for PathMatcher constructor
class PathMatcherNode;

typedef std::shared_ptr<PathMatcher> PathMatcherPtr;
typedef std::unordered_map<std::string, PathMatcherNode*>
    ServiceRootPathMatcherNodeMap;

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
  // Creates a Path Matcher with a Builder by deep-copying the builder's root
  // node.
  explicit PathMatcher(PathMatcherBuilder& builder);
  ~PathMatcher();

  MethodInfo* Lookup(const std::string& service_name,
                     const std::string& http_method, const std::string& path,
                     const std::string& query_params,
                     std::vector<VariableBinding>* variable_bindings,
                     std::string* body_field_path) const;

  MethodInfo* Lookup(const std::string& service_name,
                     const std::string& http_method,
                     const std::string& path) const;

 private:
  // A map between service names and their root path matcher nodes.
  ServiceRootPathMatcherNodeMap root_ptr_map_;
  // A root node shared by all services, i.e. paths of all services will be
  // registered to this node.
  std::unique_ptr<PathMatcherNode> default_root_ptr_;
  // Whether requests with unregistered host name will be rejected.
  bool strict_service_matching_;
  // Holds the set of custom verbs found in configured templates.
  std::set<std::string> custom_verbs_;
  // Data we store per each registered method
  struct MethodData {
    MethodInfo* method;
    std::vector<HttpTemplate::Variable> variables;
    std::string body_field_path;
  };
  // The info associated with each method. The path matcher nodes
  // will hold pointers to MethodData objects in this vector.
  std::vector<std::unique_ptr<MethodData>> methods_;

 private:
  friend class PathMatcherBuilder;
};

// This PathMatcherBuilder is used to register path-WrapperGraph pairs and
// instantiate an immutable, thread safe PathMatcher.
//
// The PathMatcherBuilder itself is NOT THREAD SAFE.
class PathMatcherBuilder {
 public:
  friend class PathMatcher;
  PathMatcherBuilder(bool strict_service_matching);
  ~PathMatcherBuilder();

  // Registers a method.
  //
  // Registrations are one-to-one. If this function is called more than once, it
  // replaces the existing method. Only the last registered method is stored.
  // Return false if path is an invalid http template.
  bool Register(std::string service_name, std::string http_method,
                std::string path, std::string body_field_path,
                MethodInfo* method);

  // Returns a shared_ptr to a thread safe PathMatcher that contains all
  // registered path-WrapperGraph pairs.
  PathMatcherPtr Build();

 private:
  // Inserts a path to a PathMatcherNode.
  void InsertPathToNode(const PathMatcherNode::PathInfo& path,
                        void* method_data, std::string service_name,
                        std::string http_method, bool mark_duplicates,
                        PathMatcherNode* root_ptr);
  // A map between service names and their root path matcher nodes.
  ServiceRootPathMatcherNodeMap root_ptr_map_;
  // A root node shared by all services, i.e. paths of all services will be
  // registered to this node.
  std::unique_ptr<PathMatcherNode> default_root_ptr_;
  // Whether requests with unregistered host name will be rejected.
  bool strict_service_matching_;
  // The set of custom verbs configured.
  // TODO: Perhaps this should not be at this level because there will
  // be multiple templates in different services on a server. Consider moving
  // this to PathMatcherNode.
  std::set<std::string> custom_verbs_;
  typedef PathMatcher::MethodData MethodData;
  std::vector<std::unique_ptr<MethodData>> methods_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_PATH_MATCHER_H_
