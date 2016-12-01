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
#ifndef API_MANAGER_CONFIG_H_
#define API_MANAGER_CONFIG_H_

#include <map>
#include <memory>
#include <set>
#include <string>

#include "google/api/service.pb.h"
#include "include/api_manager/env_interface.h"
#include "include/api_manager/method_call_info.h"
#include "src/api_manager/method_impl.h"
#include "src/api_manager/path_matcher.h"
#include "src/api_manager/proto/server_config.pb.h"

namespace google {
namespace api_manager {

class Config {
 public:
  // Creates a configuration object from service config
  //- a serialized google::api::Service protocol buffer message
  //(in either text or binary format).
  // server_config is a buffer pointer to the server config string, it can be
  // any of protobuf format json, binary or text. It can be nullptr if there is
  // not server_config.
  static std::unique_ptr<Config> Create(ApiManagerEnvInterface *env,
                                        const std::string &service_config,
                                        const std::string &server_config);

  // Returns server_config.  nullptr if no server_config.
  const proto::ServerConfig *server_config() const {
    return server_config_.get();
  }

  // Looks-up the method config info using the given url and verb.
  const MethodInfo *GetMethodInfo(const std::string &http_method,
                                  const std::string &url) const;

  // Same as above but also returns the variable bindings extracted from the url
  // according to the configured http rule (see
  // https://github.com/googleapis/googleapis/blob/master/google/api/http.proto
  // for more details).
  MethodCallInfo GetMethodCallInfo(const std::string &http_method,
                                   const std::string &url,
                                   const std::string &query_params) const;

  const ::google::api::Service &service() const { return service_; }

  // TODO: Remove in favor of service().
  const std::string &service_name() const { return service_.name(); }

  // TODO: Remove in favor of service().
  bool HasAuth() const { return service_.has_authentication(); }

  // Returns true if the caller should try openId discovery to fetch jwksUri.
  // url is set to the openId discovery link in this case. Returns false
  // if openId discovery is not needed. This means either a valid jwksUri
  // already exists, or a previous attempt to fetch jwksUri via openId
  // discovery failed.
  bool GetJwksUri(const std::string &issuer, std::string *tryOpenId) const;

  // Set jwskUri and openIdValid for a given issuer.
  void SetJwksUri(const std::string &issuer, const std::string &jwks_uri,
                  bool openid_valid);

 private:
  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(Config);

  Config();

  // Loads the service config into protobuf.
  bool LoadService(ApiManagerEnvInterface *env,
                   const std::string &service_config);

  // Loads the server config into protobuf.
  void LoadServerConfig(ApiManagerEnvInterface *env,
                        const std::string &server_config);

  // Create MethodInfo for HTTP methods, register them to PathMatcher.
  bool LoadHttpMethods(ApiManagerEnvInterface *env, PathMatcherBuilder *pmb);

  // Add a special option method info for all URLs to support CORS.
  bool AddOptionsMethodForAllUrls(ApiManagerEnvInterface *env,
                                  PathMatcherBuilder *pmb,
                                  const std::set<std::string> &all_urls);

  // Create MethodInfo for RPC methods, register them to PathMatcher.
  bool LoadRpcMethods(ApiManagerEnvInterface *env, PathMatcherBuilder *pmb);

  // Load Authentication info to MethodInfo.
  bool LoadAuthentication(ApiManagerEnvInterface *env);

  // Load Usage info to MethodInfo.
  bool LoadUsage(ApiManagerEnvInterface *env);

  // Load SystemParameters info to MethodInfo.
  bool LoadSystemParameters(ApiManagerEnvInterface *env);

  // Gets the MethodInfoImpl creating it if necessary
  MethodInfoImpl *GetOrCreateMethodInfoImpl(const std::string &name,
                                            const std::string &api_name,
                                            const std::string &api_version);

  // Load Backend info to MethodInfo.
  bool LoadBackends(ApiManagerEnvInterface *env);

  ::google::api::Service service_;
  std::unique_ptr<proto::ServerConfig> server_config_;
  PathMatcherPtr path_matcher_;
  std::map<std::string, MethodInfoImplPtr> method_map_;
  // Maps issuer to {jwksUri, openIdValid} pair.
  // jwksUri is populated either from service config, or by openId discovery.
  // openIdValid means whether or not we need to try openId discovery to fetch
  // jwksUri for the issuer. It is set to true if jwksUri is not provided in
  // service config and we have not tried openId discovery to fetch jwksUri.
  std::map<std::string, std::pair<std::string, bool>> issuer_jwks_uri_map_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_CONFIG_H_
