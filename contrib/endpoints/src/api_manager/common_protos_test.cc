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
#include "gtest/gtest.h"

#include "google/api/service.pb.h"
#include "google/api/servicecontrol/v1/service_controller.pb.h"

// Trivial tests that instantiate common protos
// to make sure the compile and link correctly.

TEST(CommonProtos, ServiceConfig) {
  ::google::api::Service service;

  service.set_name("bookstore");

  ASSERT_EQ("bookstore", service.name());
}

TEST(CommonProtos, ServiceControl) {
  ::google::api::servicecontrol::v1::CheckRequest cr;

  cr.set_service_name("bookstore");
  cr.mutable_operation()->set_operation_name("CreateShelf");

  ASSERT_EQ("bookstore", cr.service_name());
  ASSERT_EQ("CreateShelf", cr.operation().operation_name());
}
