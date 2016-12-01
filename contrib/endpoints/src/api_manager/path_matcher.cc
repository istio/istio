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
#include "src/api_manager/path_matcher.h"

#include "include/api_manager/method.h"
#include "include/api_manager/method_call_info.h"
#include "src/api_manager/http_template.h"

#include <algorithm>
#include <sstream>
#include <string>
#include <vector>

using std::string;
using std::vector;

namespace google {
namespace api_manager {

namespace {

const char kDefaultServiceName[] = "Default";

// Converts a request path into a format that can be used to perform a request
// lookup in the PathMatcher trie. This utility method sanitizes the request
// path and then splits the path into slash separated parts. Returns an empty
// vector if the sanitized path is "/".
//
// custom_verbs is a set of configured custom verbs that are used to match
// against any custom verbs in request path. If the request_path contains a
// custom verb not found in custom_verbs, it is treated as a part of the path.
//
// - Strips off query string: "/a?foo=bar" --> "/a"
// - Collapses extra slashes: "///" --> "/"
vector<string> ExtractRequestParts(string req_path);

// Looks up on a PathMatcherNode.
PathMatcherLookupResult LookupInPathMatcherNode(const PathMatcherNode& root,
                                                const vector<string>& parts,
                                                const HttpMethod& http_method);

PathMatcherNode::PathInfo TransformHttpTemplate(const HttpTemplate& ht);

std::vector<std::string>& split(const std::string& s, char delim,
                                std::vector<std::string>& elems) {
  std::stringstream ss(s);
  std::string item;
  while (std::getline(ss, item, delim)) {
    elems.push_back(item);
  }
  return elems;
}

inline bool IsReservedChar(char c) {
  // Reserved characters according to RFC 6570
  switch (c) {
    case '!':
    case '#':
    case '$':
    case '&':
    case '\'':
    case '(':
    case ')':
    case '*':
    case '+':
    case ',':
    case '/':
    case ':':
    case ';':
    case '=':
    case '?':
    case '@':
    case '[':
    case ']':
      return true;
    default:
      return false;
  }
}

// Check if an ASCII character is a hex digit.  We can't use ctype's
// isxdigit() because it is affected by locale. This function is applied
// to the escaped characters in a url, not to natural-language
// strings, so locale should not be taken into account.
inline bool ascii_isxdigit(char c) {
  return ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F') ||
         ('0' <= c && c <= '9');
}

inline int hex_digit_to_int(char c) {
  /* Assume ASCII. */
  int x = static_cast<unsigned char>(c);
  if (x > '9') {
    x += 9;
  }
  return x & 0xf;
}

// This is a helper function for UrlUnescapeString. It takes a string and
// the index of where we are within that string.
//
// The function returns true if the next three characters are of the format:
// "%[0-9A-Fa-f]{2}".
//
// If the next three characters are an escaped character then this function will
// also return what character is escaped.
bool GetEscapedChar(const string& src, size_t i, bool unescape_reserved_chars,
                    char* out) {
  if (i + 2 < src.size() && src[i] == '%') {
    if (ascii_isxdigit(src[i + 1]) && ascii_isxdigit(src[i + 2])) {
      char c =
          (hex_digit_to_int(src[i + 1]) << 4) | hex_digit_to_int(src[i + 2]);
      if (!unescape_reserved_chars && IsReservedChar(c)) {
        return false;
      }
      *out = c;
      return true;
    }
  }
  return false;
}

// Unescapes string 'part' and returns the unescaped string. Reserved characters
// (as specified in RFC 6570) are not escaped if unescape_reserved_chars is
// false.
std::string UrlUnescapeString(const std::string& part,
                              bool unescape_reserved_chars) {
  std::string unescaped;
  // Check whether we need to escape at all.
  bool needs_unescaping = false;
  char ch = '\0';
  for (size_t i = 0; i < part.size(); ++i) {
    if (GetEscapedChar(part, i, unescape_reserved_chars, &ch)) {
      needs_unescaping = true;
      break;
    }
  }
  if (!needs_unescaping) {
    unescaped = part;
    return unescaped;
  }

  unescaped.resize(part.size());

  char* begin = &(unescaped)[0];
  char* p = begin;

  for (size_t i = 0; i < part.size();) {
    if (GetEscapedChar(part, i, unescape_reserved_chars, &ch)) {
      *p++ = ch;
      i += 3;
    } else {
      *p++ = part[i];
      i += 1;
    }
  }

  unescaped.resize(p - begin);
  return unescaped;
}

void ExtractBindingsFromPath(const std::vector<HttpTemplate::Variable>& vars,
                             const std::vector<std::string>& parts,
                             std::vector<VariableBinding>* bindings) {
  for (const auto& var : vars) {
    // Determine the subpath bound to the variable based on the
    // [start_segment, end_segment) segment range of the variable.
    //
    // In case of matching "**" - end_segment is negative and is relative to
    // the end such that end_segment = -1 will match all subsequent segments.
    VariableBinding binding;
    binding.field_path = var.field_path;
    // Calculate the absolute index of the ending segment in case it's negative.
    size_t end_segment = (var.end_segment >= 0)
                             ? var.end_segment
                             : parts.size() + var.end_segment + 1;
    // It is multi-part match if we have more than one segment. We also make
    // sure that a single URL segment match with ** is also considered a
    // multi-part match by checking if it->second.end_segment is negative.
    bool is_multipart =
        (end_segment - var.start_segment) > 1 || var.end_segment < 0;
    // Joins parts with "/"  to form a path string.
    for (size_t i = var.start_segment; i < end_segment; ++i) {
      // For multipart matches only unescape non-reserved characters.
      binding.value += UrlUnescapeString(parts[i], !is_multipart);
      if (i < end_segment - 1) {
        binding.value += "/";
      }
    }
    bindings->emplace_back(binding);
  }
}

void ExtractBindingsFromQueryParameters(
    const std::string& query_params, const std::set<std::string>& system_params,
    std::vector<VariableBinding>* bindings) {
  // The bindings in URL the query parameters have the following form:
  //      <field_path1>=value1&<field_path2>=value2&...&<field_pathN>=valueN
  // Query parameters may also contain system parameters such as `api_key`.
  // We'll need to ignore these. Example:
  //      book.id=123&book.author=Neal%20Stephenson&api_key=AIzaSyAz7fhBkC35D2M
  vector<std::string> params;
  split(query_params, '&', params);
  for (const auto& param : params) {
    size_t pos = param.find('=');
    if (pos != 0 && pos != std::string::npos) {
      auto name = param.substr(0, pos);
      // Make sure the query parameter is not a system parameter (e.g.
      // `api_key`) before adding the binding.
      if (system_params.find(name) == std::end(system_params)) {
        // The name of the parameter is a field path, which is a dot-delimited
        // sequence of field names that identify the (potentially deep) field
        // in the request, e.g. `book.author.name`.
        VariableBinding binding;
        split(name, '.', binding.field_path);
        binding.value = UrlUnescapeString(param.substr(pos + 1), true);
        bindings->emplace_back(std::move(binding));
      }
    }
  }
}

}  // namespace

PathMatcher::PathMatcher(PathMatcherBuilder& builder)
    : default_root_ptr_(builder.default_root_ptr_->Clone()),
      strict_service_matching_(builder.strict_service_matching_),
      custom_verbs_(builder.custom_verbs_),
      methods_(std::move(builder.methods_)) {
  for (auto key_value : builder.root_ptr_map_) {
    utils::InsertIfNotPresent(&root_ptr_map_, key_value.first,
                              key_value.second->Clone());
  }
}

PathMatcher::~PathMatcher() { utils::STLDeleteValues(&root_ptr_map_); }

// Lookup is a wrapper method for the recursive node Lookup. First, the wrapper
// splits the request path into slash-separated path parts. Next, the method
// checks that the |http_method| is supported. If not, then it returns an empty
// WrapperGraph::SharedPtr. Next, this method invokes the node's Lookup on
// the extracted |parts|. Finally, it fills the mapping from variables to their
// values parsed from the path.
// TODO: cache results by adding get/put methods here (if profiling reveals
// benefit)
MethodInfo* PathMatcher::Lookup(const string& service_name,
                                const string& http_method, const string& url,
                                const string& query_params,
                                std::vector<VariableBinding>* variable_bindings,
                                std::string* body_field_path) const {
  const vector<string> parts = ExtractRequestParts(url);

  PathMatcherNode* root_ptr = utils::FindPtrOrNull(root_ptr_map_, service_name);

  // If service_name has not been registered to ESP and strict_service_matching_
  // is set to false, tries to lookup the method in all registered services.
  if (root_ptr == nullptr && !strict_service_matching_) {
    root_ptr = default_root_ptr_.get();
  }
  if (root_ptr == nullptr) {
    return nullptr;
  }

  PathMatcherLookupResult lookup_result =
      LookupInPathMatcherNode(*root_ptr, parts, http_method);
  // In non strict match case, if not found from the map with the exact service,
  // needs to try the default one again.
  if (lookup_result.data == nullptr && !strict_service_matching_ &&
      root_ptr != default_root_ptr_.get()) {
    root_ptr = default_root_ptr_.get();
    lookup_result = LookupInPathMatcherNode(*root_ptr, parts, http_method);
  }
  // Return nullptr if nothing is found or the result is marked for duplication.
  if (lookup_result.data == nullptr || lookup_result.is_multiple) {
    return nullptr;
  }
  MethodData* method_data = reinterpret_cast<MethodData*>(lookup_result.data);
  if (variable_bindings != nullptr) {
    variable_bindings->clear();
    ExtractBindingsFromPath(method_data->variables, parts, variable_bindings);
    ExtractBindingsFromQueryParameters(
        query_params, method_data->method->system_query_parameter_names(),
        variable_bindings);
  }
  if (body_field_path != nullptr) {
    *body_field_path = method_data->body_field_path;
  }
  return method_data->method;
}

MethodInfo* PathMatcher::Lookup(const string& service_name,
                                const string& http_method,
                                const string& path) const {
  return Lookup(service_name, http_method, path, string(), nullptr, nullptr);
}

// Initializes the builder with a root Path Segment
PathMatcherBuilder::PathMatcherBuilder(bool strict_service_matching)
    : default_root_ptr_(new PathMatcherNode()),
      strict_service_matching_(strict_service_matching) {}

PathMatcherBuilder::~PathMatcherBuilder() {
  utils::STLDeleteValues(&root_ptr_map_);
}

PathMatcherPtr PathMatcherBuilder::Build() {
  return PathMatcherPtr(new PathMatcher(*this));
}

void PathMatcherBuilder::InsertPathToNode(const PathMatcherNode::PathInfo& path,
                                          void* method_data,
                                          string service_name,
                                          string http_method,
                                          bool mark_duplicates,
                                          PathMatcherNode* root_ptr) {
  if (root_ptr->InsertPath(path, http_method, service_name, method_data,
                           mark_duplicates)) {
    //    VLOG(3) << "Registered WrapperGraph for " <<
    //    http_template.as_string();
  } else {
    //    VLOG(3) << "Replaced WrapperGraph for " << http_template.as_string();
  }
}

// This wrapper converts the |http_rule| into a HttpTemplate. Then, inserts the
// template into the trie.
bool PathMatcherBuilder::Register(string service_name, string http_method,
                                  string http_template, string body_field_path,
                                  MethodInfo* method) {
  std::unique_ptr<HttpTemplate> ht(HttpTemplate::Parse(http_template));
  if (nullptr == ht) {
    return false;
  }
  PathMatcherNode::PathInfo path_info = TransformHttpTemplate(*ht);
  if (path_info.path_info().size() == 0) {
    return false;
  }
  // Create & initialize a MethodData struct. Then insert its pointer
  // into the path matcher trie.
  auto method_data = std::unique_ptr<MethodData>(new MethodData());
  method_data->method = method;
  method_data->variables = std::move(ht->Variables());
  method_data->body_field_path = std::move(body_field_path);
  // Don't mark batch methods as duplicates, since we insert them into each
  // service, and their graphs are all the same. We'll just use the first one
  // as the default. This allows batch requests on any service name to work.
  InsertPathToNode(path_info, method_data.get(), kDefaultServiceName,
                   http_method, false, default_root_ptr_.get());
  PathMatcherNode* root_ptr =
      utils::LookupOrInsertNew(&root_ptr_map_, service_name);
  InsertPathToNode(path_info, method_data.get(), service_name, http_method,
                   true, root_ptr);
  // Add the method_data to the methods_ vector for cleanup
  methods_.emplace_back(std::move(method_data));
  return true;
}

namespace {

vector<string> ExtractRequestParts(string path) {
  // Remove query parameters.
  path = path.substr(0, path.find_first_of('?'));

  // Replace last ':' with '/' to handle custom verb.
  // But not for /foo:bar/const.
  std::size_t last_colon_pos = path.find_last_of(':');
  std::size_t last_slash_pos = path.find_last_of('/');
  if (last_colon_pos != std::string::npos && last_colon_pos > last_slash_pos) {
    path[last_colon_pos] = '/';
  }

  vector<string> result;
  if (path.size() > 0) {
    split(path.substr(1), '/', result);
  }
  // Removes all trailing empty parts caused by extra "/".
  while (!result.empty() && (*(--result.end())).empty()) {
    result.pop_back();
  }
  return result;
}

PathMatcherLookupResult LookupInPathMatcherNode(const PathMatcherNode& root,
                                                const vector<string>& parts,
                                                const HttpMethod& http_method) {
  PathMatcherLookupResult result;
  root.LookupPath(parts.begin(), parts.end(), http_method, &result);
  return result;
}

PathMatcherNode::PathInfo TransformHttpTemplate(const HttpTemplate& ht) {
  PathMatcherNode::PathInfo::Builder builder;

  for (const string& part : ht.segments()) {
    builder.AppendLiteralNode(part);
  }
  if (!ht.verb().empty()) {
    builder.AppendLiteralNode(ht.verb());
  }

  return builder.Build();
}

}  // namespace

}  // namespace api_manager
}  // namespace google
