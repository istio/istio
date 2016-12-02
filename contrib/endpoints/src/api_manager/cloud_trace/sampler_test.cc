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
#include "src/api_manager/cloud_trace/cloud_trace.h"

#include <chrono>
#include <thread>

#include "google/devtools/cloudtrace/v1/trace.pb.h"
#include "gtest/gtest.h"

using google::devtools::cloudtrace::v1::TraceSpan;

namespace google {
namespace api_manager {
namespace cloud_trace {
namespace {

class SamplerTest : public ::testing::Test {};

TEST_F(SamplerTest, TestDefaultSetting) {
  Sampler sampler(0.1);

  ASSERT_TRUE(sampler.On());
  ASSERT_FALSE(sampler.On());
}

TEST_F(SamplerTest, TestLargeQps) {
  Sampler sampler(1.0);

  ASSERT_TRUE(sampler.On());
  std::this_thread::sleep_for(std::chrono::milliseconds(300));
  ASSERT_FALSE(sampler.On());
  std::this_thread::sleep_for(std::chrono::milliseconds(800));
  ASSERT_TRUE(sampler.On());
  std::this_thread::sleep_for(std::chrono::milliseconds(200));
  ASSERT_FALSE(sampler.On());
}

TEST_F(SamplerTest, TestRefresh) {
  Sampler sampler(1.0);
  ASSERT_TRUE(sampler.On());
  std::this_thread::sleep_for(std::chrono::milliseconds(500));
  ASSERT_FALSE(sampler.On());
  sampler.Refresh();
  std::this_thread::sleep_for(std::chrono::milliseconds(600));
  ASSERT_FALSE(sampler.On());
}

TEST_F(SamplerTest, TestDisabled) {
  Sampler sampler(0.0);
  ASSERT_FALSE(sampler.On());
}

}  // namespace

}  // cloud_trace
}  // namespace api_manager
}  // namespace google
