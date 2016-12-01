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
#ifndef API_MANAGER_SERVICE_CONTROL_LOGS_METRICS_LOADER_H_
#define API_MANAGER_SERVICE_CONTROL_LOGS_METRICS_LOADER_H_

#include <functional>

#include "google/api/service.pb.h"
#include "gtest/gtest_prod.h"
#include "include/api_manager/utils/status.h"

namespace google {
namespace api_manager {
namespace service_control {

class LogsMetricsLoader final {
 public:
  static utils::Status Load(const ::google::api::Service& service,
                            std::set<std::string>* logs,
                            std::set<std::string>* metrics,
                            std::set<std::string>* labels);

 private:
  LogsMetricsLoader(std::function<bool(const ::google::api::LabelDescriptor&)>
                        label_supported,
                    std::function<bool(const ::google::api::MetricDescriptor&)>
                        metric_supported)
      : label_supported_(label_supported),
        metric_supported_(metric_supported) {}

  utils::Status AddLabels(
      const ::google::protobuf::RepeatedPtrField<
          ::google::api::LabelDescriptor>& descriptors,
      std::map<std::string, const ::google::api::LabelDescriptor&>* labels);

  utils::Status AddLogLabels(
      const ::google::protobuf::RepeatedPtrField< ::google::api::LogDescriptor>&
          descriptors,
      const std::string& log_name,
      std::map<std::string, const ::google::api::LabelDescriptor&>* labels);

  utils::Status AddMonitoredResourceLabels(
      const ::google::protobuf::RepeatedPtrField<
          ::google::api::MonitoredResourceDescriptor>& descriptors,
      const std::string& monitored_resource_name,
      std::map<std::string, const ::google::api::LabelDescriptor&>* labels);

  utils::Status AddLoggingDestinations(
      const ::google::protobuf::RepeatedPtrField<
          ::google::api::Logging_LoggingDestination>& destinations,
      const ::google::protobuf::RepeatedPtrField<
          ::google::api::MonitoredResourceDescriptor>& monitored_resources,
      const ::google::protobuf::RepeatedPtrField< ::google::api::LogDescriptor>&
          log_descriptors,
      std::set<std::string>* logs,
      std::map<std::string, const ::google::api::LabelDescriptor&>* labels);

  utils::Status AddMonitoringDestinations(
      const ::google::protobuf::RepeatedPtrField<
          ::google::api::Monitoring_MonitoringDestination>& destinations,
      const ::google::protobuf::RepeatedPtrField<
          ::google::api::MonitoredResourceDescriptor>& monitored_resources,
      const ::google::protobuf::RepeatedPtrField<
          ::google::api::MetricDescriptor>& metric_descriptors,
      std::map<std::string, const ::google::api::MetricDescriptor&>* metrics,
      std::map<std::string, const ::google::api::LabelDescriptor&>* labels);

  utils::Status LoadLogsMetrics(const ::google::api::Service& service,
                                std::set<std::string>* logs,
                                std::set<std::string>* metrics,
                                std::set<std::string>* labels);

  std::function<bool(const ::google::api::LabelDescriptor&)> label_supported_;
  std::function<bool(const ::google::api::MetricDescriptor&)> metric_supported_;

 private:
  FRIEND_TEST(LogsMetricsLoader, AddDuplicateLabels);
  FRIEND_TEST(LogsMetricsLoader, AddConflictingLabels);
  FRIEND_TEST(LogsMetricsLoader, AddUnsupportedLabels);
  FRIEND_TEST(LogsMetricsLoader, AddRedundantLabels);
  FRIEND_TEST(LogsMetricsLoader, AddNoLabels);
  FRIEND_TEST(LogsMetricsLoader, AddLogLabels);
  FRIEND_TEST(LogsMetricsLoader, AddMonitoredResourceLabels);
  FRIEND_TEST(LogsMetricsLoader, AddLoggingDestinations);
  FRIEND_TEST(LogsMetricsLoader, AddMonitoringDestinations);
  FRIEND_TEST(LogsMetricsLoader, LoadLogsMetrics);
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_LOGS_METRICS_LOADER_H_
