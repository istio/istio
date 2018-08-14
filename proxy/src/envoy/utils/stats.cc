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

#include <chrono>

#include "proxy/src/envoy/utils/stats.h"

namespace Envoy {
namespace Utils {
namespace {

// The time interval for envoy stats update.
const int kStatsUpdateIntervalInMs = 10000;

}  // namespace

MixerStatsObject::MixerStatsObject(Event::Dispatcher& dispatcher,
                                   MixerFilterStats& stats,
                                   ::google::protobuf::Duration update_interval,
                                   GetStatsFunc func)
    : stats_(stats), get_stats_func_(func) {
  stats_update_interval_ =
      update_interval.seconds() * 1000 + update_interval.nanos() / 1000000;
  if (stats_update_interval_ <= 0) {
    stats_update_interval_ = kStatsUpdateIntervalInMs;
  }
  memset(&old_stats_, 0, sizeof(old_stats_));

  if (get_stats_func_) {
    timer_ = dispatcher.createTimer([this]() { OnTimer(); });
    timer_->enableTimer(std::chrono::milliseconds(stats_update_interval_));
  }
}

void MixerStatsObject::OnTimer() {
  ::istio::mixerclient::Statistics new_stats;
  bool get_stats = get_stats_func_(&new_stats);
  if (get_stats) {
    CheckAndUpdateStats(new_stats);
  }
  timer_->enableTimer(std::chrono::milliseconds(stats_update_interval_));
}

void MixerStatsObject::CheckAndUpdateStats(
    const ::istio::mixerclient::Statistics& new_stats) {
  if (new_stats.total_check_calls > old_stats_.total_check_calls) {
    stats_.total_check_calls_.add(new_stats.total_check_calls -
                                  old_stats_.total_check_calls);
  }
  if (new_stats.total_remote_check_calls >
      old_stats_.total_remote_check_calls) {
    stats_.total_remote_check_calls_.add(new_stats.total_remote_check_calls -
                                         old_stats_.total_remote_check_calls);
  }
  if (new_stats.total_blocking_remote_check_calls >
      old_stats_.total_blocking_remote_check_calls) {
    stats_.total_blocking_remote_check_calls_.add(
        new_stats.total_blocking_remote_check_calls -
        old_stats_.total_blocking_remote_check_calls);
  }
  if (new_stats.total_quota_calls > old_stats_.total_quota_calls) {
    stats_.total_quota_calls_.add(new_stats.total_quota_calls -
                                  old_stats_.total_quota_calls);
  }
  if (new_stats.total_remote_quota_calls >
      old_stats_.total_remote_quota_calls) {
    stats_.total_remote_quota_calls_.add(new_stats.total_remote_quota_calls -
                                         old_stats_.total_remote_quota_calls);
  }
  if (new_stats.total_blocking_remote_quota_calls >
      old_stats_.total_blocking_remote_quota_calls) {
    stats_.total_blocking_remote_quota_calls_.add(
        new_stats.total_blocking_remote_quota_calls -
        old_stats_.total_blocking_remote_quota_calls);
  }
  if (new_stats.total_report_calls > old_stats_.total_report_calls) {
    stats_.total_report_calls_.add(new_stats.total_report_calls -
                                   old_stats_.total_report_calls);
  }
  if (new_stats.total_remote_report_calls >
      old_stats_.total_remote_report_calls) {
    stats_.total_remote_report_calls_.add(new_stats.total_remote_report_calls -
                                          old_stats_.total_remote_report_calls);
  }

  // Copy new_stats to old_stats_ for next stats update.
  old_stats_ = new_stats;
}

}  // namespace Utils
}  // namespace Envoy
