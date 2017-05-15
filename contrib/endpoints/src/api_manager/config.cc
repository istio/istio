// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/api_manager/config.h"
#include "contrib/endpoints/src/api_manager/utils/marshalling.h"
#include "contrib/endpoints/src/api_manager/utils/stl_util.h"
#include "contrib/endpoints/src/api_manager/utils/url_util.h"

#include <iostream>
#include <map>
#include <sstream>
#include <string>

#include "google/protobuf/io/tokenizer.h"
#include "google/protobuf/io/zero_copy_stream_impl_lite.h"
#include "google/protobuf/text_format.h"

using std::map;
using std::string;

using ::google::api_manager::utils::Status;

namespace google {
namespace api_manager {

namespace {

const char http_delete[] = "DELETE";
const char http_get[] = "GET";
// const char http_head[] = "HEAD";
const char http_patch[] = "PATCH";
const char http_post[] = "POST";
const char http_put[] = "PUT";
const char http_options[] = "OPTIONS";
const string https_prefix = "https://";
const string http_prefix = "http://";
const string openid_config_path = ".well-known/openid-configuration";
const string kFirebaseAudience =
    "/google.firebase.rules.v1.FirebaseRulesService";

class NoOpErrorCollector : public ::google::protobuf::io::ErrorCollector {
  void AddError(int line, int column, const string &message) {}
};

bool ReadConfigFromString(const std::string &service_config,
                          ::google::protobuf::Message *service) {
  // Try binary serialized proto first. Due to a bug in JSON parser,
  // JSON parser may crash if presented with non-JSON data.
  if (service->ParseFromString(service_config)) {
    return true;
  }

  // Try JSON.
  Status status = utils::JsonToProto(service_config, service);
  if (status.ok()) {
    return true;
  }

  // Try text format.
  NoOpErrorCollector error_collector;
  ::google::protobuf::TextFormat::Parser parser;
  parser.RecordErrorsTo(&error_collector);
  if (parser.ParseFromString(service_config, service)) {
    return true;
  }
  return false;
}

}  // namespace

Config::Config() {}

MethodInfoImpl *Config::GetOrCreateMethodInfoImpl(const string &name,
                                                  const string &api_name,
                                                  const string &api_version) {
  std::string selector;
  std::string path;

  if (api_name.empty()) {
    selector = name;
    path = name;
  } else {
    // The corresponding selector is "{api.name}.{method.name}".
    std::ostringstream selector_builder;
    selector_builder << api_name << '.' << name;
    selector = selector_builder.str();

    // The corresponding RPC path is "/{api.name}/{method.name}".
    std::ostringstream path_builder;
    path_builder << '/' << api_name << '/' << name;
    path = path_builder.str();
  }

  auto i = method_map_.find(selector);
  if (i == std::end(method_map_)) {
    auto info =
        MethodInfoImplPtr(new MethodInfoImpl(name, api_name, api_version));
    info->set_selector(selector);
    info->set_rpc_method_full_name(path);
    i = method_map_.emplace(selector, std::move(info)).first;
  }
  return i->second.get();
}

bool Config::LoadQuotaRule(ApiManagerEnvInterface *env) {
  for (const auto &rule : service_.quota().metric_rules()) {
    auto method = utils::FindOrNull(method_map_, rule.selector());
    if (method) {
      for (auto &metric_cost : rule.metric_costs()) {
        (*method)->add_metric_cost(metric_cost.first, metric_cost.second);
      }
    } else {
      env->LogError("Metric rule with selector " + rule.selector() +
                    "is mismatched.");
      return false;
    }
  }

  return true;
}

bool Config::LoadHttpMethods(ApiManagerEnvInterface *env,
                             PathMatcherBuilder<MethodInfo *> *pmb) {
  std::set<std::string> all_urls, urls_with_options;
  // By default, allow_cors is false. This means that the default behavior
  // of ESP is to reject all "OPTIONS" requests. If customers want to enable
  // CORS, they need to set "allow_cors" to true in swagger config.
  bool allow_cors = false;
  for (const auto &endpoint : service_.endpoints()) {
    if (endpoint.name() == service_.name() && endpoint.allow_cors()) {
      allow_cors = true;
      env->LogDebug("CORS is allowed.");
      break;
    }
  }

  for (const auto &rule : service_.http().rules()) {
    const string &selector = rule.selector();
    const string *url = nullptr;
    const char *http_method = nullptr;

    switch (rule.pattern_case()) {
      case ::google::api::HttpRule::kGet:
        url = &rule.get();
        http_method = http_get;
        break;
      case ::google::api::HttpRule::kPut:
        url = &rule.put();
        http_method = http_put;
        break;
      case ::google::api::HttpRule::kPost:
        url = &rule.post();
        http_method = http_post;
        break;
      case ::google::api::HttpRule::kDelete:
        url = &rule.delete_();
        http_method = http_delete;
        break;
      case ::google::api::HttpRule::kPatch:
        url = &rule.patch();
        http_method = http_patch;
        break;
      case ::google::api::HttpRule::kCustom:
        url = &rule.custom().path();
        http_method = rule.custom().kind().c_str();
        break;
      default:
        break;
    }

    if (http_method == nullptr || url == nullptr || url->empty()) {
      env->LogError("Invalid HTTP binding encountered.");
      continue;
    }

    MethodInfoImpl *mi = GetOrCreateMethodInfoImpl(selector, "", "");

    if (!pmb->Register(http_method, *url, rule.body(), mi)) {
      string error("Invalid HTTP template: ");
      error += *url;
      env->LogError(error.c_str());
    } else if (allow_cors) {
      all_urls.insert(*url);
      if (strcmp(http_method, http_options) == 0) {
        urls_with_options.insert(*url);
      }
    }
  }

  if (!allow_cors) {
    return true;
  }

  // Remove urls with options.
  for (auto url : urls_with_options) {
    all_urls.erase(url);
  }
  return AddOptionsMethodForAllUrls(env, pmb, all_urls);
}

bool Config::AddOptionsMethodForAllUrls(ApiManagerEnvInterface *env,
                                        PathMatcherBuilder<MethodInfo *> *pmb,
                                        const std::set<std::string> &all_urls) {
  // In order to support CORS. Http method OPTIONS needs to be added to
  // the path_matcher for all urls except the ones already with options.
  // For these OPTIONS methods, auth should be disabled and
  // allow_unregistered_calls should be true.

  // All options have same selector as format: CORS.suffix.
  // Appends suffix to make sure it is not used by any http rules.
  string cors_selector_base = "CORS";
  string cors_selector = cors_selector_base;
  int n = 0;
  while (method_map_.find(cors_selector) != method_map_.end()) {
    std::ostringstream suffix;
    suffix << ++n;
    cors_selector = cors_selector_base + "." + suffix.str();
  }
  MethodInfoImpl *mi = GetOrCreateMethodInfoImpl(cors_selector, "", "");
  mi->set_auth(false);
  mi->set_allow_unregistered_calls(true);

  for (auto url : all_urls) {
    if (!pmb->Register(http_options, url, std::string(), mi)) {
      env->LogError(
          std::string("Failed to add http options template for url: " + url));
    }
  }

  return true;
}

bool Config::LoadRpcMethods(ApiManagerEnvInterface *env,
                            PathMatcherBuilder<MethodInfo *> *pmb) {
  for (const auto &api : service_.apis()) {
    if (api.name().empty()) {
      continue;
    }

    for (const auto &method : api.methods()) {
      // The name in the Api message is the package name followed by
      // the API's simple name, and the name in the Method message is
      // the simple name of the method.
      MethodInfoImpl *mi =
          GetOrCreateMethodInfoImpl(method.name(), api.name(), api.version());

      // Initialize RPC method details
      mi->set_request_type_url(method.request_type_url());
      mi->set_request_streaming(method.request_streaming());
      mi->set_response_type_url(method.response_type_url());
      mi->set_response_streaming(method.response_streaming());

      if (!pmb->Register(http_post, mi->rpc_method_full_name(), std::string(),
                         mi)) {
        string error("Invalid method: ");
        error += mi->selector();
        env->LogError(error.c_str());
      }
    }
  }
  return true;
}

bool Config::LoadAuthentication(ApiManagerEnvInterface *env) {
  // Parsing auth config.
  const ::google::api::Authentication &auth = service_.authentication();
  map<string, const ::google::api::AuthProvider *> provider_id_provider_map;
  for (const auto &provider : auth.providers()) {
    if (provider.id().empty()) {
      env->LogError("Missing id field in AuthProvider.");
      continue;
    }
    if (provider.issuer().empty()) {
      string error = "Missing issuer field for provider: " + provider.id();
      env->LogError(error.c_str());
      continue;
    }
    if (!provider.jwks_uri().empty()) {
      SetJwksUri(provider.issuer(), provider.jwks_uri(), false);
    } else {
      SetJwksUri(provider.issuer(), string(), true);
    }
    provider_id_provider_map[provider.id()] = &provider;
  }

  for (const auto &rule : auth.rules()) {
    auto method = utils::FindOrNull(method_map_, rule.selector());
    if (method == nullptr) {
      std::string error = "Not HTTP rule defined for: " + rule.selector();
      env->LogError(error.c_str());
      continue;
    }

    // Allows disabling auth for a method when auth is enabled
    // globally for the API.
    (*method)->set_auth(rule.requirements_size() > 0);
    for (const auto &requirement : rule.requirements()) {
      const string &provider_id = requirement.provider_id();
      if (provider_id.empty()) {
        std::string error =
            "Missing provider_id field in requirements for: " + rule.selector();
        env->LogError(error.c_str());
        continue;
      }
      auto provider =
          utils::FindPtrOrNull(provider_id_provider_map, provider_id);
      if (provider == nullptr) {
        std::string error = "Undefined provider_id: " + provider_id;
        env->LogError(error.c_str());
      } else {
        const std::string &audiences = provider->audiences().empty()
                                           ? requirement.audiences()
                                           : provider->audiences();
        (*method)->addAudiencesForIssuer(provider->issuer(), audiences);
      }
    }
  }
  return true;
}

bool Config::LoadUsage(ApiManagerEnvInterface *env) {
  for (const auto &rule : service_.usage().rules()) {
    auto method = utils::FindOrNull(method_map_, rule.selector());
    if (method) {
      (*method)->set_allow_unregistered_calls(rule.allow_unregistered_calls());
    } else {
      std::string error = "Not HTTP rule defined for: " + rule.selector();
      env->LogError(error.c_str());
    }
  }
  return true;
}

bool Config::LoadSystemParameters(ApiManagerEnvInterface *env) {
  for (auto &rule : service_.system_parameters().rules()) {
    auto method = utils::FindOrNull(method_map_, rule.selector());
    if (method) {
      for (auto parameter : rule.parameters()) {
        if (parameter.name().empty()) {
          std::string error = "Missing parameter name for: " + rule.selector();
          env->LogError(error.c_str());
        } else {
          if (!parameter.http_header().empty()) {
            (*method)->add_http_header_parameter(parameter.name(),
                                                 parameter.http_header());
          }
          if (!parameter.url_query_parameter().empty()) {
            (*method)->add_url_query_parameter(parameter.name(),
                                               parameter.url_query_parameter());
          }
        }
      }
      (*method)->process_system_parameters();
    } else {
      std::string error = "Not HTTP rule defined for: " + rule.selector();
      env->LogError(error.c_str());
    }
  }
  // For each method compile a set of system query parameter names.
  // PathMatcher uses this set to ignore system query parameters when building
  // variable bindings.
  for (auto &m : method_map_) {
    m.second->ProcessSystemQueryParameterNames();
  }
  return true;
}

bool Config::LoadBackends(ApiManagerEnvInterface *env) {
  for (auto &rule : service_.backend().rules()) {
    if (rule.address().empty()) {
      continue;
    }
    auto method = utils::FindOrNull(method_map_, rule.selector());
    if (method) {
      if (!(*method)->backend_address().empty()) {
        std::string error =
            "Duplicate a backend address for selector: " + rule.selector();
        env->LogError(error.c_str());
        continue;
      }
      (*method)->set_backend_address(rule.address());
    } else {
      std::string error =
          "No method matching backend selector: " + rule.selector();
      env->LogError(error.c_str());
    }
  }
  return true;
}

bool Config::LoadService(ApiManagerEnvInterface *env,
                         const std::string &service_config) {
  if (!service_config.empty()) {
    if (!ReadConfigFromString(service_config, &service_)) {
      env->LogError("Cannot load ESP configuration protocol buffer.");
      return false;
    }

    if (service_.name().empty()) {
      env->LogError("Service name not specified in the API configuration.");
      return false;
    }

    string tf;
    ::google::protobuf::TextFormat::PrintToString(service_, &tf);
    env->LogDebug(tf.c_str());
    return true;
  }
  return false;
}

std::shared_ptr<proto::ServerConfig> Config::LoadServerConfig(
    ApiManagerEnvInterface *env, const std::string &server_config) {
  std::shared_ptr<proto::ServerConfig> config;
  if (!server_config.empty()) {
    config = std::make_shared<proto::ServerConfig>();
    if (!ReadConfigFromString(server_config, config.get())) {
      env->LogError("Cannot load server configuration protocol buffer.");
      config.reset();
    }
  }
  return config;
}

// This is only used for unit-test.
std::unique_ptr<Config> Config::Create(ApiManagerEnvInterface *env,
                                       const std::string &service_config,
                                       const std::string &server_config) {
  std::unique_ptr<Config> config = Create(env, service_config);
  if (config) {
    config->set_server_config(LoadServerConfig(env, server_config));
  }
  return config;
}

std::unique_ptr<Config> Config::Create(ApiManagerEnvInterface *env,
                                       const std::string &service_config) {
  std::unique_ptr<Config> config(new Config);
  if (!config->LoadService(env, service_config)) {
    return nullptr;
  }
  PathMatcherBuilder<MethodInfo *> pmb;
  // Load apis before http rules to store API versions
  if (!config->LoadRpcMethods(env, &pmb)) {
    return nullptr;
  }
  if (!config->LoadHttpMethods(env, &pmb)) {
    return nullptr;
  }
  config->path_matcher_ = pmb.Build();
  if (!config->LoadAuthentication(env)) {
    return nullptr;
  }
  if (!config->LoadUsage(env)) {
    return nullptr;
  }
  if (!config->LoadSystemParameters(env)) {
    return nullptr;
  }
  if (!config->LoadBackends(env)) {
    return nullptr;
  }
  if (!config->LoadQuotaRule(env)) {
    return nullptr;
  }
  return config;
}

const MethodInfo *Config::GetMethodInfo(const string &http_method,
                                        const string &url) const {
  return path_matcher_ == nullptr ? nullptr
                                  : path_matcher_->Lookup(http_method, url);
}

MethodCallInfo Config::GetMethodCallInfo(
    const std::string &http_method, const std::string &url,
    const std::string &query_params) const {
  MethodCallInfo call_info;
  if (path_matcher_ == nullptr) {
    call_info.method_info = nullptr;
  } else {
    call_info.method_info = path_matcher_->Lookup(
        http_method, url, query_params, &call_info.variable_bindings,
        &call_info.body_field_path);
  }
  return call_info;
}

bool Config::GetJwksUri(const string &issuer, string *url) const {
  std::string iss = utils::GetUrlContent(issuer);
  auto it = issuer_jwks_uri_map_.find(iss);
  if (it == issuer_jwks_uri_map_.end()) {
    // Unknown issuer.
    *url = string();
    return false;
  }
  if (!it->second.first.empty()) {
    // jwksUri is not empty, return it.
    *url = it->second.first;
    return false;
  }
  // jwksUri is empty.
  if (it->second.second) {
    // openIdValid is true. We need to try open ID discovery to fetch jwksUri.
    // Set url to discovery url.
    if (issuer.compare(0, https_prefix.size(), https_prefix) != 0 &&
        issuer.compare(0, http_prefix.size(), http_prefix) != 0) {
      // OpenID standard requires that "issuer" is a URL. However,
      // not all providers strictly follow the standard. For example,
      // Google ID token's issuer field is "accounts.google.com".
      *url = https_prefix + issuer;
    } else {
      *url = issuer;
    }
    if ((*url).back() != '/') {
      *url += '/';
    }
    *url += openid_config_path;
    return true;
  }
  // jwksUri is empty and openIdValid is false. This means that we have
  // already tried openId discovery but failed to fetch jwkUri.
  *url = string();
  return false;
}

void Config::SetJwksUri(const string &issuer, const string &jwks_uri,
                        bool openid_valid) {
  std::string iss = utils::GetUrlContent(issuer);
  if (!iss.empty()) {
    issuer_jwks_uri_map_[iss] = std::make_pair(jwks_uri, openid_valid);
  }
}

std::string Config::GetFirebaseServer() {
  // Server config overwrites service config.
  if (server_config_ != nullptr &&
      server_config_->has_api_check_security_rules_config() &&
      !server_config_->api_check_security_rules_config()
           .firebase_server()
           .empty()) {
    return server_config_->api_check_security_rules_config().firebase_server();
  }

  if (service_.has_experimental() &&
      service_.experimental().has_authorization() &&
      !service_.experimental().authorization().provider().empty()) {
    return service_.experimental().authorization().provider();
  }
  return "";
}

std::string Config::GetFirebaseAudience() {
  return GetFirebaseServer() + kFirebaseAudience;
}

}  // namespace api_manager
}  // namespace google
