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

#include "google/protobuf/io/zero_copy_stream_impl_lite.h"
#include "google/protobuf/text_format.h"

#include "gmock/gmock.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {
namespace service_control {

namespace {

using ::google::api::LabelDescriptor;
using ::google::api::LogDescriptor;
using ::google::api::MetricDescriptor;
using ::google::api::Service;
using ::google::api_manager::utils::Status;
using ::google::protobuf::RepeatedPtrField;

using ::testing::_;
using ::testing::Eq;
using ::testing::Pair;
using ::testing::Property;
using ::testing::UnorderedElementsAre;

template <class Message>
Message Parse(const char *text) {
  ::google::protobuf::TextFormat::Parser parser;
  ::google::protobuf::io::ArrayInputStream input(text, strlen(text));
  Message msg;
  bool success = parser.Parse(&input, &msg);
  EXPECT_TRUE(success);
  return msg;
}

const char unsupported_prefix[] = "unsupported/";

bool StartsWith(const std::string &value, const char *prefix, size_t size) {
  return value.compare(0, size, prefix) == 0;
}

bool IsLabelSupported(const LabelDescriptor &ld) {
  return !StartsWith(ld.key(), unsupported_prefix,
                     sizeof(unsupported_prefix) - 1);
}

bool IsMetricSupported(const MetricDescriptor &md) {
  return !StartsWith(md.name(), unsupported_prefix,
                     sizeof(unsupported_prefix) - 1);
}

typedef std::map<std::string, const LabelDescriptor &> LabelMap;
typedef std::map<std::string, const MetricDescriptor &> MetricMap;

}  // namespace

TEST(LogsMetricsLoaderTest, StartsWith) {
  ASSERT_TRUE(StartsWith("abc", "ab", 2));
  ASSERT_FALSE(
      StartsWith("abc", unsupported_prefix, sizeof(unsupported_prefix) - 1));
}

TEST(LogsMetricsLoaderTest, IsSupported) {
  LabelDescriptor ld;
  ld.set_key("a");
  ASSERT_TRUE(IsLabelSupported(ld));
  ld.set_key("unsupported");
  ASSERT_TRUE(IsLabelSupported(ld));
  ld.set_key("supported/foo");
  ASSERT_TRUE(IsLabelSupported(ld));
  ld.set_key("unsupported/");
  ASSERT_FALSE(IsLabelSupported(ld));
  ld.set_key("unsupported/foo");
  ASSERT_FALSE(IsLabelSupported(ld));

  MetricDescriptor md;
  md.set_name("a");
  ASSERT_TRUE(IsMetricSupported(md));
  md.set_name("unsupported");
  ASSERT_TRUE(IsMetricSupported(md));
  md.set_name("supported/foo");
  ASSERT_TRUE(IsMetricSupported(md));
  md.set_name("unsupported/");
  ASSERT_FALSE(IsMetricSupported(md));
  md.set_name("unsupported/foo");
  ASSERT_FALSE(IsMetricSupported(md));
}

// Configuration contains duplicate labels. We de-dupe them.
TEST(LogsMetricsLoader, AddDuplicateLabels) {
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);

  {
    RepeatedPtrField<LabelDescriptor> lds;
    lds.Add()->set_key("duplicate");
    lds.Add()->set_key("duplicate");

    LabelMap labels;
    ASSERT_TRUE(lml.AddLabels(lds, &labels).ok());
    ASSERT_THAT(labels, UnorderedElementsAre(Pair("duplicate", _)));
  }
  {
    // Unsupported duplicates are not added.
    RepeatedPtrField<LabelDescriptor> lds;
    lds.Add()->set_key("unsupported/duplicate");
    lds.Add()->set_key("unsupported/duplicate");

    LabelMap empty;
    ASSERT_TRUE(lml.AddLabels(lds, &empty).ok());
    ASSERT_TRUE(empty.empty());
  }
}

// Set of labels so far collected contains label which is in conflict with label
// newly added.
TEST(LogsMetricsLoader, AddConflictingLabels) {
  const char log_descriptor[] =  // We parse LogDescriptor but all we need are
                                 // its labels
      "labels {"
      "  key: \"api-key\""  // lexicographically before 'conflicting'
      "}"
      "labels {"
      "  key: \"conflicting\""
      "  value_type: BOOL"
      "}"
      "labels {"
      "  key: \"request-size\""
      "  value_type: INT64"
      "}";

  LogDescriptor ld = Parse<LogDescriptor>(log_descriptor);
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);

  LabelDescriptor ld_string;
  ld_string.set_key("conflicting");
  ld_string.set_value_type(LabelDescriptor::STRING);
  LabelMap conflict({{"conflicting", ld_string}});
  ASSERT_FALSE(lml.AddLabels(ld.labels(), &conflict).ok());

  // Assert that the label map was not modified.
  ASSERT_THAT(conflict,
              UnorderedElementsAre(
                  Pair("conflicting", Property(&LabelDescriptor::value_type,
                                               Eq(LabelDescriptor::STRING)))));
}

TEST(LogsMetricsLoader, AddUnsupportedLabels) {
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);
  RepeatedPtrField<LabelDescriptor> lds;
  lds.Add()->set_key("unsupported/foo");
  lds.Add()->set_key("supported/baz");
  lds.Add()->set_key("unsupported/bar");

  LabelMap labels;
  ASSERT_TRUE(lml.AddLabels(lds, &labels).ok());
  ASSERT_THAT(labels, UnorderedElementsAre(Pair("supported/baz", _)));
}

TEST(LogsMetricsLoader, AddRedundantLabels) {
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);
  const std::string name("redundant");
  RepeatedPtrField<LabelDescriptor> lds;
  lds.Add()->set_key(name);

  LabelDescriptor redundant;
  redundant.set_key(name);
  LabelMap labels({{name, redundant}});

  ASSERT_TRUE(lml.AddLabels(lds, &labels).ok());
  ASSERT_THAT(labels, UnorderedElementsAre(Pair(name, _)));
}

TEST(LogsMetricsLoader, AddNoLabels) {
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);
  RepeatedPtrField<LabelDescriptor> lds;

  // Adding no labels must work.
  LabelMap labels;
  ASSERT_TRUE(lml.AddLabels(lds, &labels).ok());
  EXPECT_TRUE(labels.empty());
}

TEST(LogsMetricsLoader, AddLogLabels) {
  const char service_config[] =  // Only contains logs, that's all we need.
      "logs {"
      "  name: \"endpoints\""
      "  labels {"
      "    key: \"unsupported/endpoints\""
      "  }"
      "  labels {"
      "    key: \"supported/endpoints\""
      "  }"
      "}"
      "logs {"
      "  name: \"startpoints\""
      "  labels {"
      "    key: \"supported/startpoints\""
      "  }"
      "  labels {"
      "    key: \"unsupported/startpoints\""
      "  }"
      "}";

  Service service = Parse<Service>(service_config);
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);

  // Test that only supported labels from the named log are added.
  {
    LabelMap labels;
    ASSERT_TRUE(lml.AddLogLabels(service.logs(), "endpoints", &labels).ok());
    ASSERT_THAT(labels, UnorderedElementsAre(Pair("supported/endpoints", _)));
  }
  {
    LabelMap labels;
    ASSERT_TRUE(lml.AddLogLabels(service.logs(), "startpoints", &labels).ok());
    ASSERT_THAT(labels, UnorderedElementsAre(Pair("supported/startpoints", _)));
  }

  // Referencing log not present in the config --> error.
  {
    LabelMap labels;
    ASSERT_FALSE(lml.AddLogLabels(service.logs(), "notfound", &labels).ok());
    ASSERT_TRUE(labels.empty());
  }
}

TEST(LogsMetricsLoader, AddMonitoredResourceLabels) {
  const char service_config[] =  // Only contains monitored resources, that's
                                 // all we need.
      "monitored_resources {"
      "  type: \"endpoints.googleapis.com/endpoints\""
      "  labels {"
      "    key: \"unsupported/endpoints\""
      "  }"
      "  labels {"
      "    key: \"supported/endpoints\""
      "  }"
      "}"
      "monitored_resources {"
      "  type: \"startpoints.googleapis.com/startpoints\""
      "  labels {"
      "    key: \"supported/startpoints\""
      "  }"
      "  labels {"
      "    key: \"unsupported/startpoints\""
      "  }"
      "}";

  Service service = Parse<Service>(service_config);
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);

  // Test that only supported labels from the specific monitored resource are
  // added.
  {
    LabelMap labels;
    ASSERT_TRUE(lml.AddMonitoredResourceLabels(
                       service.monitored_resources(),
                       "endpoints.googleapis.com/endpoints", &labels)
                    .ok());
    ASSERT_THAT(labels, UnorderedElementsAre(Pair("supported/endpoints", _)));
  }
  {
    LabelMap labels;
    ASSERT_TRUE(lml.AddMonitoredResourceLabels(
                       service.monitored_resources(),
                       "startpoints.googleapis.com/startpoints", &labels)
                    .ok());
    ASSERT_THAT(labels, UnorderedElementsAre(Pair("supported/startpoints", _)));
  }

  // Referencing non-existent monitored resource --> error.
  {
    LabelMap labels;
    ASSERT_FALSE(lml.AddMonitoredResourceLabels(
                        service.monitored_resources(),
                        "endpoints.googleapis.com/notfound", &labels)
                     .ok());
    ASSERT_TRUE(labels.empty());
  }
}

TEST(LogsMetricsLoader, AddLoggingDestinations) {
  const char service_config[] =
      // A valid log with one supported label.
      "logs {"
      "  name: \"endpoints-log\""
      "  labels {"
      "    key: \"supported/endpoints-log-label\""
      "  }"
      "  labels {"
      "    key: \"unsupported/endpoints-log-label\""
      "  }"
      "}"

      // A log otherwise unreferenced in the config.
      "logs {"
      "  name: \"unreferenced-log\""
      "  labels: {"
      "    key: \"supported/unreferenced-log-label\""
      "  }"
      "}"

      // Only one valid monitored resource.
      "monitored_resources {"
      "  type: \"endpoints.googleapis.com/endpoints\""
      "  labels {"
      "    key: \"unsupported/endpoints\""
      "  }"
      "  labels {"
      "    key: \"supported/endpoints\""
      "  }"
      "}"

      // Logging section
      "logging {"
      // Invalid logging destination (bad resource, log).
      "  producer_destinations {"
      "    monitored_resource: \"bad-monitored-resource\""
      "    logs: \"bad-monitored-resource-log\""
      "  }"

      // Partly valid logging destination (good resource, some good logs).
      "  producer_destinations {"
      "    monitored_resource: \"endpoints.googleapis.com/endpoints\""
      "    logs: \"bad-endpoints-log\""
      "    logs: \"endpoints-log\""
      "  }"
      "}";

  Service service = Parse<Service>(service_config);
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);

  std::set<std::string> logs;
  LabelMap labels;

  Status s = lml.AddLoggingDestinations(
      service.logging().producer_destinations(), service.monitored_resources(),
      service.logs(), &logs, &labels);
  ASSERT_TRUE(s.ok());

  // Only one log was referenced, and valid.
  ASSERT_THAT(logs, UnorderedElementsAre("endpoints-log"));

  // Only the supported labels from the monitored resource and the log
  // are going to be reported.
  ASSERT_THAT(labels,
              UnorderedElementsAre(Pair("supported/endpoints", _),
                                   Pair("supported/endpoints-log-label", _)));
}

TEST(LogsMetricsLoader, AddMonitoringDestinations) {
  const char service_config[] =
      // A valid metric with one supported label.
      "metrics {"
      "  name: \"supported/endpoints-metric\""
      "  labels {"
      "    key: \"supported/endpoints-metric-label\""
      "  }"
      "  labels {"
      "    key: \"unsupported/endpoints-metric-label\""
      "  }"
      "}"

      // An unsupported metric with a supported label. The supported label won't
      // be reported because the metric isn't.
      "metrics {"
      "  name: \"unsupported/unsupported-endpoints-metric\""
      "  labels {"
      "    key: \"supported/unreferenced-metric-label\""
      "  }"
      "}"

      // Supported metric used by non-existent monitored resource. Thus, it
      // won't be reported.
      "metrics {"
      "  name: \"supported/non-existent-resource-metric\""
      "  labels {"
      "    key: \"supported/non-existent-resource-metric-label\""
      "  }"
      "}"

      // Only one valid monitored resource.
      "monitored_resources {"
      "  type: \"endpoints.googleapis.com/endpoints\""
      "  labels {"
      "    key: \"unsupported/endpoints\""
      "  }"
      "  labels {"
      "    key: \"supported/endpoints\""
      "  }"
      "}"

      // Monitoring section. Use consumer destinations only for the purpose of
      // the test.
      "monitoring {"
      "  consumer_destinations {"
      "    monitored_resource: \"endpoints.googleapis.com/endpoints\""
      "    metrics: \"supported/endpoints-metric\""
      "    metrics: \"unsupported/unsupported-endpoints-metric\""
      "    metrics: \"supported/unknown-metric\""
      "  }"

      // One valid metric but on unrecognized monitored resource. This should be
      // skipped.
      "  consumer_destinations {"
      "    monitored_resource: \"endpoints.googleapis.com/non-existent\""
      "    metrics: \"supported/endpoints-metric\""
      "    metrics: \"unsupported/unsupported-endpoints-metric\""
      "    metrics: \"supported/unknown-metric\""
      "    metrics: \"supported/non-existent-resource-metric\""
      "  }"
      "}";

  Service service = Parse<Service>(service_config);
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);

  MetricMap metrics;
  LabelMap labels;
  Status s = lml.AddMonitoringDestinations(
      service.monitoring().consumer_destinations(),
      service.monitored_resources(), service.metrics(), &metrics, &labels);
  ASSERT_TRUE(s.ok());

  // Only the supported metrics from valid monitored resources have been added.
  ASSERT_THAT(metrics,
              UnorderedElementsAre(Pair("supported/endpoints-metric", _)));

  // Only the supported labels from the monitored resource and from the metric
  // are recognized.
  ASSERT_THAT(labels, UnorderedElementsAre(
                          Pair("supported/endpoints", _),
                          Pair("supported/endpoints-metric-label", _)));
}

TEST(LogsMetricsLoader, LoadLogsMetrics) {
  const char service_config[] =
      // A valid log with one supported label.
      "logs {"
      "  name: \"endpoints-log\""
      "  labels {"
      "    key: \"supported/endpoints-log-label\""
      "  }"
      "  labels {"
      "    key: \"unsupported/endpoints-log-label\""
      "  }"
      "}"

      // Shared metric.
      "metrics {"
      "  name: \"supported/endpoints-metric\""
      "  labels {"
      "    key: \"supported/endpoints-metric-label\""
      "  }"
      "  labels {"
      "    key: \"unsupported/endpoints-metric-label\""
      "  }"
      "}"
      // Consumer metric.
      "metrics {"
      "  name: \"supported/endpoints-consumer-metric\""
      "  labels {"
      "    key: \"supported/endpoints-metric-label\""
      "  }"
      "  labels {"
      "    key: \"supported/endpoints-consumer-metric-label\""
      "  }"
      "}"
      // Producer metric.
      "metrics {"
      "  name: \"supported/endpoints-producer-metric\""
      "  labels {"
      "    key: \"supported/endpoints-metric-label\""
      "  }"
      "  labels {"
      "    key: \"supported/endpoints-producer-metric-label\""
      "  }"
      "}"

      // Only one valid monitored resource.
      "monitored_resources {"
      "  type: \"endpoints.googleapis.com/endpoints\""
      "  labels {"
      "    key: \"unsupported/endpoints\""
      "  }"
      "  labels {"
      "    key: \"supported/endpoints\""
      "  }"
      "}"

      // Logging section
      "logging {"
      "  producer_destinations {"
      "    monitored_resource: \"endpoints.googleapis.com/endpoints\""
      "    logs: \"endpoints-log\""
      "  }"
      "}"

      // Monitoring section.
      "monitoring {"
      "  consumer_destinations {"
      "    monitored_resource: \"endpoints.googleapis.com/endpoints\""
      "    metrics: \"supported/endpoints-consumer-metric\""
      "    metrics: \"supported/endpoints-metric\""
      "  }"

      // One valid metric but on unrecognized monitored resource. This should be
      // skipped.
      "  producer_destinations {"
      "    monitored_resource: \"endpoints.googleapis.com/endpoints\""
      "    metrics: \"supported/endpoints-producer-metric\""
      "    metrics: \"supported/endpoints-metric\""
      "  }"
      "}";

  Service service = Parse<Service>(service_config);
  LogsMetricsLoader lml(IsLabelSupported, IsMetricSupported);

  std::set<std::string> logs, metrics, labels;
  Status s = lml.LoadLogsMetrics(service, &logs, &metrics, &labels);
  ASSERT_TRUE(s.ok());

  ASSERT_THAT(logs, UnorderedElementsAre("endpoints-log"));
  ASSERT_THAT(metrics,
              UnorderedElementsAre("supported/endpoints-metric",
                                   "supported/endpoints-consumer-metric",
                                   "supported/endpoints-producer-metric"));
  ASSERT_THAT(
      labels,
      UnorderedElementsAre(
          "supported/endpoints",               // from monitored resource
          "supported/endpoints-log-label",     // from log
          "supported/endpoints-metric-label",  // from both metrics
          "supported/endpoints-consumer-metric-label",  // from consumer metric
          "supported/endpoints-producer-metric-label"   // from producer metric
          ));
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
