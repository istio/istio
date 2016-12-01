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
#include "src/api_manager/service_control/logs_metrics_loader.h"

#include <algorithm>

#include "src/api_manager/service_control/proto.h"

namespace google {
namespace api_manager {
namespace service_control {

using ::google::api::LabelDescriptor;
using ::google::api::LogDescriptor;
using ::google::api::Logging;
using ::google::api::Logging_LoggingDestination;
using ::google::api::MetricDescriptor;
using ::google::api::MonitoredResourceDescriptor;
using ::google::api::Monitoring;
using ::google::api::Monitoring_MonitoringDestination;
using ::google::api::Service;
using ::google::api_manager::utils::Status;
using ::google::protobuf::RepeatedPtrField;
using ::google::protobuf::util::error::Code;

Status LogsMetricsLoader::Load(const Service& service,
                               std::set<std::string>* logs,
                               std::set<std::string>* metrics,
                               std::set<std::string>* labels) {
  LogsMetricsLoader lml(Proto::IsLabelSupported, Proto::IsMetricSupported);
  return lml.LoadLogsMetrics(service, logs, metrics, labels);
}

Status LogsMetricsLoader::AddLabels(
    const RepeatedPtrField<LabelDescriptor>& descriptors,
    std::map<std::string, const LabelDescriptor&>* labels) {
  for (int i = 0, size = descriptors.size(); i < size; i++) {
    const LabelDescriptor& ld = descriptors.Get(i);

    // Check for pre-existing incompatible duplicates
    auto existing = labels->find(ld.key());
    if (existing != labels->end()) {
      if (existing->second.value_type() != ld.value_type()) {
        return Status(Code::INVALID_ARGUMENT,
                      "Conflicting label in the configuration: " + ld.key());
      }
    }
  }

  // Only insert the labels into the output set once we validated them.
  for (int i = 0, size = descriptors.size(); i < size; i++) {
    const LabelDescriptor& ld = descriptors.Get(i);
    if (label_supported_(ld)) {
      labels->insert(std::map<std::string, const LabelDescriptor&>::value_type(
          ld.key(), ld));
    }
  }

  return Status::OK;
}

Status LogsMetricsLoader::AddLogLabels(
    const ::google::protobuf::RepeatedPtrField<LogDescriptor>& descriptors,
    const std::string& log_name,
    std::map<std::string, const LabelDescriptor&>* labels) {
  for (int i = 0, size = descriptors.size(); i < size; i++) {
    const LogDescriptor& ld = descriptors.Get(i);
    if (ld.name() == log_name) {
      // Found the log.
      return AddLabels(ld.labels(), labels);
    }
  }
  return Status(Code::INVALID_ARGUMENT, "Log not found: " + log_name);
}

Status LogsMetricsLoader::AddMonitoredResourceLabels(
    const RepeatedPtrField<MonitoredResourceDescriptor>& descriptors,
    const std::string& monitored_resource_name,
    std::map<std::string, const LabelDescriptor&>* labels) {
  for (int i = 0, size = descriptors.size(); i < size; i++) {
    const MonitoredResourceDescriptor& mr = descriptors.Get(i);
    if (mr.type() == monitored_resource_name) {
      // Found the monitored resource.
      return AddLabels(mr.labels(), labels);
    }
  }
  return Status(Code::INVALID_ARGUMENT,
                "Monitored resource not found: " + monitored_resource_name);
}

Status LogsMetricsLoader::AddLoggingDestinations(
    const RepeatedPtrField<Logging_LoggingDestination>& destinations,
    const RepeatedPtrField<MonitoredResourceDescriptor>& monitored_resources,
    const RepeatedPtrField<LogDescriptor>& log_descriptors,
    std::set<std::string>* logs,
    std::map<std::string, const LabelDescriptor&>* labels) {
  Status s = Status::OK;
  for (int d = 0, dsize = destinations.size(); d < dsize; d++) {
    const Logging_LoggingDestination& ld = destinations.Get(d);

    s = AddMonitoredResourceLabels(monitored_resources, ld.monitored_resource(),
                                   labels);
    if (!s.ok()) {
      continue;  // Skip bad monitored resource.
    }

    // Store names of logs ESP should log into.
    for (int l = 0, lsize = ld.logs_size(); l < lsize; l++) {
      const std::string& log_name = ld.logs(l);
      s = AddLogLabels(log_descriptors, log_name, labels);
      if (!s.ok()) {
        continue;  // Skip bad log.
      }
      logs->insert(log_name);
    }
  }
  return Status::OK;
}

const MetricDescriptor* FindMetricDescriptor(
    const RepeatedPtrField<MetricDescriptor>& metric_descriptors,
    const std::string& metric_name) {
  auto it = std::find_if(metric_descriptors.begin(), metric_descriptors.end(),
                         [&metric_name](const MetricDescriptor& md) {
                           return md.name() == metric_name;
                         });
  return it != metric_descriptors.end() ? &*it : nullptr;
}

Status LogsMetricsLoader::AddMonitoringDestinations(
    const RepeatedPtrField<Monitoring_MonitoringDestination>& destinations,
    const RepeatedPtrField<MonitoredResourceDescriptor>& monitored_resources,
    const RepeatedPtrField<MetricDescriptor>& metric_descriptors,
    std::map<std::string, const MetricDescriptor&>* metrics,
    std::map<std::string, const LabelDescriptor&>* labels) {
  Status s = Status::OK;

  for (int d = 0, dsize = destinations.size(); d < dsize; d++) {
    const Monitoring_MonitoringDestination& md = destinations.Get(d);
    s = AddMonitoredResourceLabels(monitored_resources, md.monitored_resource(),
                                   labels);
    if (!s.ok()) {
      continue;  // Skip bad monitored resource.
    }

    for (int m = 0, msize = md.metrics_size(); m < msize; m++) {
      const std::string& metric_name = md.metrics(m);
      const MetricDescriptor* metric_descriptor =
          FindMetricDescriptor(metric_descriptors, metric_name);
      if (metric_descriptor == nullptr ||
          !metric_supported_(*metric_descriptor)) {
        continue;  // Skip unrecognized or unsupported metric.
      }

      // Add metric specific labels.
      s = AddLabels(metric_descriptor->labels(), labels);
      if (!s.ok()) {
        continue;  // Skip bad metric.
      }

      // Insert the metric to make sure we report it.
      metrics->insert(
          std::map<std::string, const MetricDescriptor&>::value_type(
              metric_name, *metric_descriptor));
    }
  }

  return Status::OK;
}

Status LogsMetricsLoader::LoadLogsMetrics(const Service& service,
                                          std::set<std::string>* logs,
                                          std::set<std::string>* metrics,
                                          std::set<std::string>* labels) {
  std::map<std::string, const LabelDescriptor&> labels_map;
  std::map<std::string, const MetricDescriptor&> metrics_map;

  Status s = Status::OK;

  // ESP logs into all producer destination logs.
  const Logging& logging = service.logging();
  s = AddLoggingDestinations(logging.producer_destinations(),
                             service.monitored_resources(), service.logs(),
                             logs, &labels_map);
  if (!s.ok()) return s;

  // ESP reports producer and consumer metrics.
  const Monitoring& monitoring = service.monitoring();
  s = AddMonitoringDestinations(monitoring.producer_destinations(),
                                service.monitored_resources(),
                                service.metrics(), &metrics_map, &labels_map);
  if (!s.ok()) return s;
  s = AddMonitoringDestinations(monitoring.consumer_destinations(),
                                service.monitored_resources(),
                                service.metrics(), &metrics_map, &labels_map);
  if (!s.ok()) return s;

  std::set<std::string> metrics_set;
  for (auto it = metrics_map.begin(), end = metrics_map.end(); it != end;
       it++) {
    metrics->insert(it->first);
  }

  std::set<std::string> labels_set;
  for (auto it = labels_map.begin(), end = labels_map.end(); it != end; it++) {
    labels->insert(it->first);
  }

  return Status::OK;
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
