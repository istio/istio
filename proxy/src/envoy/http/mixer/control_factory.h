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
#include "proxy/src/envoy/http/mixer/control.h"
#include "proxy/src/envoy/utils/stats.h"

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// Envoy stats perfix for HTTP filter stats.
const std::string kHttpStatsPrefix("http_mixer_filter.");

}  // namespace

// This object is globally per listener.
// HttpMixerControl is created per-thread by this object.
class ControlFactory : public Logger::Loggable<Logger::Id::config> {
 public:
  ControlFactory(std::unique_ptr<Config> config,
                 Server::Configuration::FactoryContext& context)
      : config_(std::move(config)),
        tls_(context.threadLocal().allocateSlot()),
        stats_{ALL_MIXER_FILTER_STATS(
            POOL_COUNTER_PREFIX(context.scope(), kHttpStatsPrefix))} {
    Upstream::ClusterManager& cm = context.clusterManager();
    Runtime::RandomGenerator& random = context.random();
    Stats::Scope& scope = context.scope();
    tls_->set([this, &cm, &random, &scope](Event::Dispatcher& dispatcher)
                  -> ThreadLocal::ThreadLocalObjectSharedPtr {
      return std::make_shared<Control>(*config_, cm, dispatcher, random, scope,
                                       stats_);
    });
  }

  Control& control() { return tls_->getTyped<Control>(); }

 private:
  // Own the config object.
  std::unique_ptr<Config> config_;
  // Thread local slot.
  ThreadLocal::SlotPtr tls_;
  // This stats object.
  Utils::MixerFilterStats stats_;
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
