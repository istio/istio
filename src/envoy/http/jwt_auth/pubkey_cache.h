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

#ifndef JWT_AUTH_PUBKEY_CACHE_H
#define JWT_AUTH_PUBKEY_CACHE_H

#include <chrono>
#include <unordered_map>

#include "src/envoy/http/jwt_auth/config.pb.h"
#include "src/envoy/http/jwt_auth/jwt.h"

namespace Envoy {
namespace Http {
namespace JwtAuth {
namespace {
// Default cache expiration time in 5 minutes.
const int kPubkeyCacheExpirationSec = 600;
}  // namespace

// Struct to hold an issuer cache item.
class PubkeyCacheItem {
 public:
  PubkeyCacheItem(const Config::JWT& jwt_config) : jwt_config_(jwt_config) {
    // Convert proto repeated fields to std::set.
    for (const auto& aud : jwt_config_.audiences()) {
      audiences_.insert(aud);
    }
  }

  // Return true if cached pubkey is expired.
  bool Expired() const {
    return std::chrono::steady_clock::now() >= expiration_time_;
  }

  // Get the JWT config.
  const Config::JWT& jwt_config() const { return jwt_config_; }

  // Get the pubkey object.
  const Pubkeys* pubkey() const { return pubkey_.get(); }

  // Check if an audience is allowed.
  bool IsAudienceAllowed(const std::vector<std::string>& jwt_audiences) {
    if (audiences_.empty()) {
      return true;
    }
    for (const auto& aud : jwt_audiences) {
      if (audiences_.find(aud) != audiences_.end()) {
        return true;
      }
    }
    return false;
  }

  // Set a pubkey as string.
  Status SetKey(const std::string& pubkey_str) {
    auto pubkey = Pubkeys::CreateFrom(pubkey_str, Pubkeys::JWKS);
    if (pubkey->GetStatus() != Status::OK) {
      return pubkey->GetStatus();
    }
    pubkey_ = std::move(pubkey);

    expiration_time_ = std::chrono::steady_clock::now();
    if (jwt_config_.has_public_key_cache_duration()) {
      const auto& duration = jwt_config_.public_key_cache_duration();
      expiration_time_ += std::chrono::seconds(duration.seconds()) +
                          std::chrono::nanoseconds(duration.nanos());
    } else {
      expiration_time_ += std::chrono::seconds(kPubkeyCacheExpirationSec);
    }
    return Status::OK;
  }

 private:
  // The issuer config
  const Config::JWT& jwt_config_;
  // Use set for fast lookup
  std::set<std::string> audiences_;
  // The generated pubkey object.
  std::unique_ptr<Pubkeys> pubkey_;
  // The pubkey expiration time.
  std::chrono::steady_clock::time_point expiration_time_;
};

// Pubkey cache
class PubkeyCache {
 public:
  // Load the config from envoy config.
  PubkeyCache(const Config::AuthFilterConfig& config) {
    for (const auto& jwt : config.jwts()) {
      pubkey_cache_map_.emplace(jwt.issuer(), jwt);
    }
  }

  // Lookup issuer cache map.
  PubkeyCacheItem* LookupByIssuer(const std::string& name) {
    auto it = pubkey_cache_map_.find(name);
    if (it == pubkey_cache_map_.end()) {
      return nullptr;
    }
    return &it->second;
  }

 private:
  // The public key cache map indexed by issuer.
  std::unordered_map<std::string, PubkeyCacheItem> pubkey_cache_map_;
};

}  // namespace JwtAuth
}  // namespace Http
}  // namespace Envoy

#endif  // JWT_AUTH_PUBKEY_CACHE_H
