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

#pragma once

#include "envoy/event/dispatcher.h"
#include "envoy/event/timer.h"
#include "envoy/stats/stats_macros.h"
#include "include/istio/mixerclient/client.h"

namespace Envoy {
namespace Utils {

/**
 * All mixer filter stats. @see stats_macros.h
 */
// clang-format off
#define ALL_MIXER_FILTER_STATS(COUNTER)                                       \
  COUNTER(total_check_calls)                                                  \
  COUNTER(total_remote_check_calls)                                           \
  COUNTER(total_blocking_remote_check_calls)                                  \
  COUNTER(total_quota_calls)                                                  \
  COUNTER(total_remote_quota_calls)                                           \
  COUNTER(total_blocking_remote_quota_calls)                                  \
  COUNTER(total_report_calls)                                                 \
  COUNTER(total_remote_report_calls)
// clang-format on

/**
 * Struct definition for all mixer filter stats. @see stats_macros.h
 */
struct MixerFilterStats {
  ALL_MIXER_FILTER_STATS(GENERATE_COUNTER_STRUCT)
};

typedef std::function<bool(::istio::mixerclient::Statistics* s)> GetStatsFunc;

// MixerStatsObject maintains statistics for number of check, quota and report
// calls issued by a mixer filter.
class MixerStatsObject {
 public:
  MixerStatsObject(Event::Dispatcher& dispatcher, MixerFilterStats& stats,
                   ::google::protobuf::Duration update_interval,
                   GetStatsFunc func);

 private:
  // This function is invoked when timer event fires.
  void OnTimer();

  // Compares old stats with new stats and updates envoy stats.
  void CheckAndUpdateStats(const ::istio::mixerclient::Statistics& new_stats);

  // A set of Envoy stats for the number of check, quota and report calls.
  MixerFilterStats& stats_;
  // Stores a function which gets statistics from mixer controller.
  GetStatsFunc get_stats_func_;

  // stats from last call to get_stats_func_. This is needed to calculate the
  // variances of stats and update envoy stats.
  ::istio::mixerclient::Statistics old_stats_;

  // These members are used for creating a timer which update Envoy stats
  // periodically.
  ::Envoy::Event::TimerPtr timer_;

  // Time interval at which Envoy stats get updated. If stats update interval
  // from config is larger than 0, then store configured interval here.
  // Otherwise, set interval to 10 seconds.
  int stats_update_interval_;
};

}  // namespace Utils
}  // namespace Envoy
