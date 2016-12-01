// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
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
