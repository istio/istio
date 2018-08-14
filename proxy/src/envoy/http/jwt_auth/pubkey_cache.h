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

#include <chrono>
#include <unordered_map>

#include "common/common/logger.h"
#include "common/config/datasource.h"
#include "envoy/config/filter/http/jwt_auth/v2alpha1/config.pb.h"
#include "proxy/src/envoy/http/jwt_auth/jwt.h"

namespace Envoy {
namespace Http {
namespace JwtAuth {
namespace {
// Default cache expiration time in 5 minutes.
const int kPubkeyCacheExpirationSec = 600;

// HTTP Protocol scheme prefix in JWT aud claim.
const std::string kHTTPSchemePrefix("http://");

// HTTPS Protocol scheme prefix in JWT aud claim.
const std::string kHTTPSSchemePrefix("https://");

// Coped from @envoy/source/common/config/datasource.cc
// changed to use
// ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::DataSource
std::string ReadDataStore(
    const ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::DataSource&
        source,
    bool allow_empty) {
  switch (source.specifier_case()) {
    case ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::DataSource::
        kInlineBytes:
      return source.inline_bytes();
    case ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::DataSource::
        kInlineString:
      return source.inline_string();
    default:
      if (!allow_empty) {
        throw EnvoyException(
            fmt::format("Unexpected DataSource::specifier_case(): {}",
                        source.specifier_case()));
      }
      return "";
  }
}

}  // namespace

// Struct to hold an issuer cache item.
class PubkeyCacheItem : public Logger::Loggable<Logger::Id::filter> {
 public:
  PubkeyCacheItem(
      const ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::JwtRule&
          jwt_config)
      : jwt_config_(jwt_config) {
    // Convert proto repeated fields to std::set.
    for (const auto& aud : jwt_config_.audiences()) {
      audiences_.insert(SanitizeAudience(aud));
    }

    auto inline_jwks = ReadDataStore(jwt_config_.local_jwks(), true);
    if (!inline_jwks.empty()) {
      Status status = SetKey(inline_jwks,
                             // inline jwks never expires.
                             std::chrono::steady_clock::time_point::max());
      if (status != Status::OK) {
        ENVOY_LOG(warn, "Invalid inline jwks for issuer: {}, jwks: {}",
                  jwt_config_.issuer(), inline_jwks);
      }
    }
  }

  // Return true if cached pubkey is expired.
  bool Expired() const {
    return std::chrono::steady_clock::now() >= expiration_time_;
  }

  // Get the JWT config.
  const ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::JwtRule&
  jwt_config() const {
    return jwt_config_;
  }

  // Get the pubkey object.
  const Pubkeys* pubkey() const { return pubkey_.get(); }

  // Check if an audience is allowed.
  bool IsAudienceAllowed(const std::vector<std::string>& jwt_audiences) {
    if (audiences_.empty()) {
      return true;
    }
    for (const auto& aud : jwt_audiences) {
      if (audiences_.find(SanitizeAudience(aud)) != audiences_.end()) {
        return true;
      }
    }
    return false;
  }

  Status SetRemoteJwks(const std::string& pubkey_str) {
    return SetKey(pubkey_str, GetRemoteJwksExpirationTime());
  }

 private:
  // Get the expiration time for remote JWKS
  std::chrono::steady_clock::time_point GetRemoteJwksExpirationTime() const {
    auto expire = std::chrono::steady_clock::now();
    if (jwt_config_.has_remote_jwks() &&
        jwt_config_.remote_jwks().has_cache_duration()) {
      const auto& duration = jwt_config_.remote_jwks().cache_duration();
      expire += std::chrono::seconds(duration.seconds()) +
                std::chrono::nanoseconds(duration.nanos());
    } else {
      expire += std::chrono::seconds(kPubkeyCacheExpirationSec);
    }
    return expire;
  }

  // Set a pubkey as string.
  Status SetKey(const std::string& pubkey_str,
                std::chrono::steady_clock::time_point expire) {
    auto pubkey = Pubkeys::CreateFrom(pubkey_str, Pubkeys::JWKS);
    if (pubkey->GetStatus() != Status::OK) {
      return pubkey->GetStatus();
    }
    pubkey_ = std::move(pubkey);
    expiration_time_ = expire;
    return Status::OK;
  }

  // Searches protocol scheme prefix and trailing slash from aud, and
  // returns aud without these prefix and suffix.
  std::string SanitizeAudience(const std::string& aud) {
    int beg = 0;
    int end = aud.length() - 1;
    bool sanitize_aud = false;
    // Point beg to first character after protocol scheme prefix in audience.
    if (aud.compare(0, kHTTPSchemePrefix.length(), kHTTPSchemePrefix) == 0) {
      beg = kHTTPSchemePrefix.length();
      sanitize_aud = true;
    } else if (aud.compare(0, kHTTPSSchemePrefix.length(),
                           kHTTPSSchemePrefix) == 0) {
      beg = kHTTPSSchemePrefix.length();
      sanitize_aud = true;
    }
    // Point end to trailing slash in aud.
    if (end >= 0 && aud[end] == '/') {
      --end;
      sanitize_aud = true;
    }
    if (sanitize_aud) {
      return aud.substr(beg, end - beg + 1);
    }
    return aud;
  }

  // The issuer config
  const ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::JwtRule&
      jwt_config_;
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
  PubkeyCache(const ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::
                  JwtAuthentication& config) {
    for (const auto& jwt : config.rules()) {
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
