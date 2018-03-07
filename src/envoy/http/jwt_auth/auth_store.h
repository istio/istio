/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#pragma once

#include "common/common/logger.h"
#include "envoy/server/filter_config.h"
#include "envoy/thread_local/thread_local.h"
#include "src/envoy/http/jwt_auth/config.pb.h"
#include "src/envoy/http/jwt_auth/pubkey_cache.h"
#include "src/envoy/http/jwt_auth/token_extractor.h"

namespace Envoy {
namespace Http {
namespace JwtAuth {

// The JWT auth store object to store config and caches.
// It only has pubkey_cache for now. In the future it will have token cache.
// It is per-thread and stored in thread local.
class JwtAuthStore : public ThreadLocal::ThreadLocalObject {
 public:
  // Load the config from envoy config.
  JwtAuthStore(const Config::AuthFilterConfig& config)
      : config_(config), pubkey_cache_(config_), token_extractor_(config_) {}

  // Get the Config.
  const Config::AuthFilterConfig& config() const { return config_; }

  // Get the pubkey cache.
  PubkeyCache& pubkey_cache() { return pubkey_cache_; }

  // Get the private token extractor.
  const JwtTokenExtractor& token_extractor() const { return token_extractor_; }

 private:
  // Store the config.
  const Config::AuthFilterConfig& config_;
  // The public key cache, indexed by issuer.
  PubkeyCache pubkey_cache_;
  // The object to extract token.
  JwtTokenExtractor token_extractor_;
};

// The factory to create per-thread auth store object.
class JwtAuthStoreFactory : public Logger::Loggable<Logger::Id::config> {
 public:
  JwtAuthStoreFactory(const Config::AuthFilterConfig& config,
                      Server::Configuration::FactoryContext& context)
      : config_(config), tls_(context.threadLocal().allocateSlot()) {
    tls_->set(
        [this](Event::Dispatcher&) -> ThreadLocal::ThreadLocalObjectSharedPtr {
          return std::make_shared<JwtAuthStore>(config_);
        });
    ENVOY_LOG(info, "Loaded JwtAuthConfig: {}", config_.DebugString());
  }

  // Get per-thread auth store object.
  JwtAuthStore& store() { return tls_->getTyped<JwtAuthStore>(); }

 private:
  // The auth config.
  Config::AuthFilterConfig config_;
  // Thread local slot to store per-thread auth store
  ThreadLocal::SlotPtr tls_;
};

}  // namespace JwtAuth
}  // namespace Http
}  // namespace Envoy
