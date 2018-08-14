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

#include "proxy/src/envoy/tcp/mixer/control.h"

namespace Envoy {
namespace Tcp {
namespace Mixer {
namespace {

// Envoy stats perfix for TCP filter stats.
const std::string kTcpStatsPrefix("tcp_mixer_filter.");

}  // namespace

class ControlFactory : public Logger::Loggable<Logger::Id::filter> {
 public:
  ControlFactory(std::unique_ptr<Config> config,
                 Server::Configuration::FactoryContext& context)
      : config_(std::move(config)),
        cm_(context.clusterManager()),
        tls_(context.threadLocal().allocateSlot()),
        stats_(generateStats(kTcpStatsPrefix, context.scope())),
        uuid_(context.random().uuid()) {
    Runtime::RandomGenerator& random = context.random();
    Stats::Scope& scope = context.scope();
    tls_->set([this, &random, &scope](Event::Dispatcher& dispatcher)
                  -> ThreadLocal::ThreadLocalObjectSharedPtr {
      return ThreadLocal::ThreadLocalObjectSharedPtr(
          new Control(*config_, cm_, dispatcher, random, scope, stats_, uuid_));
    });
  }

  // Get the per-thread control
  Control& control() { return tls_->getTyped<Control>(); }

 private:
  // Generates stats struct.
  static Utils::MixerFilterStats generateStats(const std::string& name,
                                               Stats::Scope& scope) {
    return {ALL_MIXER_FILTER_STATS(POOL_COUNTER_PREFIX(scope, name))};
  }

  // The config object
  std::unique_ptr<Config> config_;
  // The cluster manager
  Upstream::ClusterManager& cm_;
  // the thread local slots
  ThreadLocal::SlotPtr tls_;
  // The statistics struct.
  Utils::MixerFilterStats stats_;
  // UUID of the Envoy TCP mixer filter.
  const std::string uuid_;
};

}  // namespace Mixer
}  // namespace Tcp
}  // namespace Envoy
