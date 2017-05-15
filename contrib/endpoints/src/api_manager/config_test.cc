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
#include "contrib/endpoints/src/api_manager/config.h"
#include "contrib/endpoints/src/api_manager/mock_api_manager_environment.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace {

// Integration tests use text format or JSON config. Test the binary proto code
// here.
TEST(Config, CreateFromBinaryProto) {
  ::google::api::Service service;
  service.set_name("service-name");
  ::google::api::Http *http = service.mutable_http();
  ::google::api::HttpRule *rule = http->add_rules();

  rule->set_get("/path/**");
  rule->set_selector("Paths.Get");

  ::google::protobuf::Api *api = service.add_apis();
  api->set_name("Paths");
  api->add_methods()->set_name("Get");

  std::string proto(service.SerializeAsString());
  ::testing::NiceMock<MockApiManagerEnvironment> env;
  std::unique_ptr<Config> config = Config::Create(&env, proto, "");
  ASSERT_NE(nullptr, config.get());

  const MethodInfo *method = config->GetMethodInfo("GET", "/path/a/b/c");
  ASSERT_NE(nullptr, method);

  std::string name = method->selector();
  ASSERT_EQ("Paths.Get", name);
  ASSERT_FALSE(method->allow_unregistered_calls());

  method = config->GetMethodInfo("POST", "/Paths/Get");
  ASSERT_NE(nullptr, method);

  ASSERT_EQ("Paths.Get", method->selector());
  ASSERT_FALSE(method->allow_unregistered_calls());
}

static const char kServerConfig[] = R"(
service_control_config {
  check_aggregator_config {
    cache_entries: 1000
    flush_interval_ms: 10
    response_expiration_ms: 20
  }
  report_aggregator_config {
    cache_entries: 1020
    flush_interval_ms: 15
  }
}
)";

const char kServiceNameConfig[] = "name: \"service-one\"\n";

TEST(Config, ServerConfigProto) {
  ::testing::NiceMock<MockApiManagerEnvironmentWithLog> env;

  std::unique_ptr<Config> config =
      Config::Create(&env, kServiceNameConfig, kServerConfig);

  EXPECT_TRUE(config);

  auto server_config = config->server_config();
  EXPECT_NE(nullptr, server_config);

  ASSERT_EQ(1000, server_config->service_control_config()
                      .check_aggregator_config()
                      .cache_entries());
  ASSERT_EQ(15, server_config->service_control_config()
                    .report_aggregator_config()
                    .flush_interval_ms());
}

static const char kInvalidServerConfig[] = R"(
service_control_config {
  type: 1
  config {
    cache_entries: 1020
    flush_interval_ms: 15
  }
}
)";

TEST(Config, InvalidServerConfigProto) {
  ::testing::NiceMock<MockApiManagerEnvironmentWithLog> env;

  std::unique_ptr<Config> config =
      Config::Create(&env, kServiceNameConfig, kInvalidServerConfig);

  EXPECT_TRUE(config);

  auto server_config = config->server_config();
  EXPECT_EQ(nullptr, server_config);
}

const char invalid_config[] = "this is an invalid service config";

TEST(Config, InvalidConfig) {
  ::testing::NiceMock<MockApiManagerEnvironment> env;

  std::unique_ptr<Config> config = Config::Create(&env, invalid_config, "");
  ASSERT_EQ(nullptr, config.get());
}

TEST(Config, NoServiceName) {
  // Create a non-empty service config.
  ::google::api::Service service;
  service.mutable_control()->set_environment("servicecontrol.googleapis.com");

  std::string service_binary;
  ASSERT_TRUE(service.SerializeToString(&service_binary));

  ::testing::NiceMock<MockApiManagerEnvironment> env;
  std::unique_ptr<Config> config = Config::Create(&env, service_binary, "");
  ASSERT_EQ(nullptr, config.get());
}

static const char http_config_with_some_errors[] =
    "name: \"config-test\"\n"
    "http: {\n"
    "  rules: {\n"
    "    selector: \"Invalid.Get\"\n"
    "    get: \"/invalid/{open\"\n"  // Invalid template.
    "  }\n"
    "  rules: {\n"
    "    selector: \"Invalid.Post\""
    "    post: \"\""  // Invalid binding.
    "  }\n"
    "  rules: {\n"
    "    selector: \"Valid.Method\"\n"
    "    get: \"/valid/*\""
    "  }\n"
    "}\n"
    "apis: {\n"
    "  methods: {\n"
    "    name: \"MethodInUnnamedApi\"\n"
    "  }\n"
    "}\n"
    "apis: {\n"
    "  name: \"test.api\"\n"
    "  methods: {\n"
    "    name: \"ValidMethod\"\n"
    "  }\n"
    "}";

TEST(Config, HttpConfigLoading) {
  ::testing::NiceMock<MockApiManagerEnvironment> env;
  std::unique_ptr<Config> config =
      Config::Create(&env, http_config_with_some_errors, "");
  ASSERT_NE(nullptr, config.get());

  const MethodInfo *method = config->GetMethodInfo("GET", "/valid/foo");
  ASSERT_NE(nullptr, method);

  method = config->GetMethodInfo("POST", "/MethodInUnnamedApi");
  ASSERT_EQ(nullptr, method);

  method = config->GetMethodInfo("POST", "//MethodInUnnamedApi");
  ASSERT_EQ(nullptr, method);

  method = config->GetMethodInfo("POST", "/test.api/ValidMethod");
  ASSERT_NE(nullptr, method);
}

static const char auth_config_with_some_errors[] =
    "name: \"no-provider-test\"\n"
    "authentication {\n"
    "  providers {\n"
    "    id: \"\"\n"  // empty provider id
    "    issuer: \"issuer@gserviceaccount.com\""
    "  }\n"
    "  providers {\n"
    "    id: \"provider-id\"\n"
    "    issuer: \"\""  // empty issuer
    "  }\n"
    "  rules {\n"
    "    selector: \"NoProvider.Method\"\n"
    "    requirements {\n"
    "      provider_id: \"test_auth\"\n"
    "      audiences: \"ok_audience\"\n"
    "    }\n"
    "  }\n"
    "}\n"
    "http {\n"
    "  rules {\n"
    "    selector: \"Good.Method\"\n"
    "    get: \"/method/*\"\n"
    "  }\n"
    "}\n";

TEST(Config, MethodWithUnknownProvider) {
  ::testing::NiceMock<MockApiManagerEnvironment> env;

  std::unique_ptr<Config> config =
      Config::Create(&env, auth_config_with_some_errors, "");
  ASSERT_NE(nullptr, config.get());

  const MethodInfo *method = config->GetMethodInfo("GET", "/method/good");
  ASSERT_NE(nullptr, method);
  ASSERT_EQ("Good.Method", method->name());
}

static const char usage_config[] =
    "name: \"usage-config\"\n"
    "usage {\n"
    "  rules {\n"
    "    selector: \"Xyz.Method1\"\n"
    "    allow_unregistered_calls: true\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method2\"\n"
    "    allow_unregistered_calls: false\n"
    "  }\n"
    "}\n"
    "http {\n"
    "  rules {\n"
    "    selector: \"Xyz.Method1\"\n"
    "    get: \"/xyz/method1/*\"\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method2\"\n"
    "    get: \"/xyz/method2/*\"\n"
    "  }\n"
    "}\n";

TEST(Config, TestLoadUsage) {
  ::testing::NiceMock<MockApiManagerEnvironment> env;

  std::unique_ptr<Config> config = Config::Create(&env, usage_config, "");
  ASSERT_NE(nullptr, config.get());

  const MethodInfo *method1 = config->GetMethodInfo("GET", "/xyz/method1/abc");
  ASSERT_EQ("Xyz.Method1", method1->name());
  ASSERT_TRUE(method1->allow_unregistered_calls());

  const MethodInfo *method2 = config->GetMethodInfo("GET", "/xyz/method2/abc");
  ASSERT_EQ("Xyz.Method2", method2->name());
  ASSERT_FALSE(method2->allow_unregistered_calls());
}

static const char custom_method_config[] =
    "name: \"custom-method-config\"\n"
    "http {\n"
    "  rules {\n"
    "    selector: \"Xyz.Method1\"\n"
    "    get: \"/xyz/method1/*\"\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method2\"\n"
    "    get: \"/xyz/method2/*\"\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method3\"\n"
    "    custom: {\n"
    "      kind: \"OPTIONS\"\n"
    "      path: \"/xyz/method3/*\"\n"
    "    }\n"
    "  }\n"
    "}\n";

TEST(Config, TestCustomMethod) {
  ::testing::NiceMock<MockApiManagerEnvironment> env;

  std::unique_ptr<Config> config =
      Config::Create(&env, custom_method_config, "");
  ASSERT_NE(nullptr, config.get());

  const MethodInfo *method1 = config->GetMethodInfo("GET", "/xyz/method1/abc");
  ASSERT_EQ("Xyz.Method1", method1->name());

  const MethodInfo *method2 = config->GetMethodInfo("GET", "/xyz/method2/abc");
  ASSERT_EQ("Xyz.Method2", method2->name());

  const MethodInfo *method3 =
      config->GetMethodInfo("OPTIONS", "/xyz/method3/abc");
  ASSERT_EQ("Xyz.Method3", method3->name());
}

static const char auth_config[] =
    "name: \"auth-config-test\"\n"
    "authentication {\n"
    "  providers {\n"
    "    id: \"provider-id1\"\n"
    "    issuer: \"issuer1@gserviceaccount.com\"\n"
    "    jwks_uri: \"https://www.googleapis.com/jwks_uri1\"\n"
    "    audiences: \"ok_audience1\"\n"
    "  }\n"
    "  providers {\n"
    "    id: \"provider-id2\"\n"
    "    issuer: \"issuer2@gserviceaccount.com\"\n"
    "    jwks_uri: \"https://www.googleapis.com/jwks_uri2\"\n"
    "  }\n"
    "  providers {\n"
    "    id: \"esp-auth0\"\n"
    "    issuer: \"esp-jwk.auth0.com\"\n"
    "  }\n"
    "  providers {\n"
    "    id: \"google-x\"\n"
    "    issuer: \"accounts.google.com/\"\n"
    "  }\n"
    "  providers {\n"
    "    id: \"localhost\"\n"
    "    issuer: \"http://localhost\"\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method1\"\n"
    "    requirements {\n"
    "      provider_id: \"provider-id1\"\n"
    "    }\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method2\"\n"
    "    requirements {\n"
    "      provider_id: \"provider-id2\"\n"
    "      audiences: \"ok_audience2\"\n"
    "    }\n"
    "  }\n"
    "}\n"
    "http {\n"
    "  rules {\n"
    "    selector: \"Xyz.Method1\"\n"
    "    get: \"/xyz/method1/*\"\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method2\"\n"
    "    get: \"/xyz/method2/*\"\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method3\"\n"
    "    get: \"/xyz/method3/*\"\n"
    "  }\n"
    "}\n";

TEST(Config, TestLoadAuthentication) {
  MockApiManagerEnvironmentWithLog env;

  std::unique_ptr<Config> config = Config::Create(&env, auth_config, "");
  ASSERT_NE(nullptr, config.get());

  const MethodInfo *method1 = config->GetMethodInfo("GET", "/xyz/method1/abc");
  ASSERT_EQ("Xyz.Method1", method1->name());
  ASSERT_TRUE(method1->auth());
  ASSERT_TRUE(method1->isIssuerAllowed("issuer1@gserviceaccount.com"));
  ASSERT_FALSE(method1->isIssuerAllowed("issuer2@gserviceaccount.com"));
  ASSERT_TRUE(method1->isAudienceAllowed("issuer1@gserviceaccount.com",
                                         {"ok_audience1"}));
  ASSERT_FALSE(method1->isAudienceAllowed("issuer1@gserviceaccount.com",
                                          {"ok_audience2"}));

  const MethodInfo *method2 = config->GetMethodInfo("GET", "/xyz/method2/abc");
  ASSERT_EQ("Xyz.Method2", method2->name());
  ASSERT_TRUE(method2->auth());
  ASSERT_FALSE(method2->isIssuerAllowed("issuer1@gserviceaccount.com"));
  ASSERT_TRUE(method2->isIssuerAllowed("issuer2@gserviceaccount.com"));
  ASSERT_FALSE(method2->isAudienceAllowed("issuer2@gserviceaccount.com",
                                          {"ok_audience1"}));
  ASSERT_TRUE(method2->isAudienceAllowed("issuer2@gserviceaccount.com",
                                         {"ok_audience2"}));

  const MethodInfo *method3 = config->GetMethodInfo("GET", "/xyz/method3/abc");
  ASSERT_EQ("Xyz.Method3", method3->name());
  ASSERT_FALSE(method3->auth());

  std::string url;
  bool ret = config->GetJwksUri("issuer1@gserviceaccount.com", &url);
  ASSERT_EQ("https://www.googleapis.com/jwks_uri1", url);
  ASSERT_FALSE(ret);
  ret = config->GetJwksUri("issuer2@gserviceaccount.com", &url);
  ASSERT_EQ("https://www.googleapis.com/jwks_uri2", url);
  ASSERT_FALSE(ret);
  // A falure test: unknown issuer.
  ret = config->GetJwksUri("issuer3@gserviceaccount.com", &url);
  ASSERT_EQ("", url);
  ASSERT_FALSE(ret);
  // Returns openId discovery URL
  ret = config->GetJwksUri("esp-jwk.auth0.com", &url);
  ASSERT_EQ("https://esp-jwk.auth0.com/.well-known/openid-configuration", url);
  ASSERT_TRUE(ret);

  ret = config->GetJwksUri("https://accounts.google.com/", &url);
  ASSERT_EQ("https://accounts.google.com/.well-known/openid-configuration",
            url);
  ASSERT_TRUE(ret);

  ret = config->GetJwksUri("http://localhost", &url);
  ASSERT_EQ("http://localhost/.well-known/openid-configuration", url);
  ASSERT_TRUE(ret);

  ret = config->GetJwksUri("accounts.google.com/", &url);
  ASSERT_EQ("https://accounts.google.com/.well-known/openid-configuration",
            url);
  ASSERT_TRUE(ret);
}

static const char system_parameter_config[] =
    "name: \"usage-config\"\n"
    "system_parameters {\n"
    "  rules {\n"
    "    selector: \"Xyz.Method1\"\n"
    "    parameters {\n"
    "       name: \"name1\"\n"
    "       http_header: \"Header-Key1\"\n"
    "       url_query_parameter: \"paramer_key1\"\n"
    "    }\n"
    "    parameters {\n"
    "       name: \"name2\"\n"
    "       http_header: \"Header-Key2\"\n"
    "       url_query_parameter: \"paramer_key2\"\n"
    "    }\n"
    "  }\n"
    "}\n"
    "http {\n"
    "  rules {\n"
    "    selector: \"Xyz.Method1\"\n"
    "    get: \"/xyz/method1/*\"\n"
    "  }\n"
    "  rules {\n"
    "    selector: \"Xyz.Method2\"\n"
    "    get: \"/xyz/method2/*\"\n"
    "  }\n"
    "}\n";

TEST(Config, TestLoadSystemParameters) {
  MockApiManagerEnvironmentWithLog env;

  std::unique_ptr<Config> config =
      Config::Create(&env, system_parameter_config, "");
  ASSERT_NE(nullptr, config.get());

  const MethodInfo *method1 = config->GetMethodInfo("GET", "/xyz/method1/abc");
  ASSERT_EQ("Xyz.Method1", method1->name());
  ASSERT_EQ("Header-Key1", *method1->http_header_parameters("name1")->begin());
  ASSERT_EQ("Header-Key2", *method1->http_header_parameters("name2")->begin());
  ASSERT_EQ("paramer_key1", *method1->url_query_parameters("name1")->begin());
  ASSERT_EQ("paramer_key2", *method1->url_query_parameters("name2")->begin());

  const MethodInfo *method2 = config->GetMethodInfo("GET", "/xyz/method2/abc");
  ASSERT_EQ("Xyz.Method2", method2->name());
  ASSERT_EQ(nullptr, method2->http_header_parameters("name1"));
  ASSERT_EQ(nullptr, method2->http_header_parameters("name2"));
  ASSERT_EQ(nullptr, method2->url_query_parameters("name1"));
  ASSERT_EQ(nullptr, method2->url_query_parameters("name2"));
}

static const char backends_config[] =
    "name: \"backends-config\"\n"
    "backend {\n"
    "  rules {\n"
    "    selector: \"test.api.MethodWithBackend\"\n"
    "    address: \"TestBackend:TestPort\"\n"
    "  }\n"
    "}\n"
    "apis {\n"
    "  name: \"test.api\"\n"
    "  methods {\n"
    "    name: \"MethodWithBackend\"\n"
    "  }\n"
    "  methods {\n"
    "    name: \"MethodWithoutBackend\"\n"
    "  }\n"
    "}\n";

TEST(Config, LoadBackends) {
  MockApiManagerEnvironmentWithLog env;

  std::unique_ptr<Config> config = Config::Create(&env, backends_config, "");
  ASSERT_TRUE(config);

  const MethodInfo *method_with_backend =
      config->GetMethodInfo("POST", "/test.api/MethodWithBackend");
  ASSERT_NE(nullptr, method_with_backend);
  EXPECT_EQ("TestBackend:TestPort", method_with_backend->backend_address());

  const MethodInfo *method_without_backend =
      config->GetMethodInfo("POST", "/test.api/MethodWithoutBackend");
  ASSERT_NE(nullptr, method_without_backend);
  EXPECT_TRUE(method_without_backend->backend_address().empty());
}

TEST(Config, RpcMethodsWithHttpRules) {
  MockApiManagerEnvironmentWithLog env;

  const char config_text[] = R"(
    name : "BookstoreApi"
    apis {
      name: "Bookstore"
      methods {
        name: "ListShelves"
        request_type_url: "types.googleapis.com/google.protobuf.Empty"
        response_type_url: "types.googleapis.com/Bookstore.ListShelvesResponse"
      }
      methods {
        name: "CreateShelves"
        request_streaming: true
        request_type_url: "types.googleapis.com/Bookstore.Shelf"
        response_streaming: true
        response_type_url: "types.googleapis.com/Bookstore.Shelf"
      }
    }
    http {
      rules {
        selector: "Bookstore.ListShelves"
        get: "/shelves"
      }
      rules {
        selector: "Bookstore.CreateShelves"
        post: "/shelves"
      }
    }
  )";

  std::unique_ptr<Config> config = Config::Create(&env, config_text, "");
  ASSERT_TRUE(config);

  const MethodInfo *list_shelves =
      config->GetMethodInfo("POST", "/Bookstore/ListShelves");

  ASSERT_NE(nullptr, list_shelves);
  EXPECT_EQ("/Bookstore/ListShelves", list_shelves->rpc_method_full_name());
  EXPECT_EQ("types.googleapis.com/google.protobuf.Empty",
            list_shelves->request_type_url());
  EXPECT_EQ(false, list_shelves->request_streaming());
  EXPECT_EQ("types.googleapis.com/Bookstore.ListShelvesResponse",
            list_shelves->response_type_url());
  EXPECT_EQ(false, list_shelves->response_streaming());

  const MethodInfo *create_shelves =
      config->GetMethodInfo("POST", "/Bookstore/CreateShelves");

  ASSERT_NE(nullptr, create_shelves);
  EXPECT_EQ("/Bookstore/CreateShelves", create_shelves->rpc_method_full_name());
  EXPECT_EQ("types.googleapis.com/Bookstore.Shelf",
            create_shelves->request_type_url());
  EXPECT_EQ(true, create_shelves->request_streaming());
  EXPECT_EQ("types.googleapis.com/Bookstore.Shelf",
            create_shelves->response_type_url());
  EXPECT_EQ(true, create_shelves->response_streaming());

  // Matching through http rule path must yield the same method
  EXPECT_EQ(list_shelves, config->GetMethodInfo("GET", "/shelves"));
  EXPECT_EQ(create_shelves, config->GetMethodInfo("POST", "/shelves"));
}

TEST(Config, RpcMethodsWithHttpRulesAndVariableBindings) {
  MockApiManagerEnvironmentWithLog env;

  const char config_text[] =
      R"(
      name : "BookstoreApi"
      apis {
        name: "Bookstore"
        methods {
          name: "ListShelves"
          request_type_url: "types.googleapis.com/google.protobuf.Empty"
          response_type_url: "types.googleapis.com/Bookstore.ListShelvesResponse"
        }
        methods {
          name: "ListBooks"
          request_type_url: "types.googleapis.com/google.protobuf.Empty"
          response_type_url: "types.googleapis.com/Bookstore.ListBooksResponse"
        }
        methods {
          name: "CreateBook"
          request_type_url: "types.googleapis.com/Bookstore.CreateBookRequest"
          response_type_url: "types.googleapis.com/Bookstore.Book"
        }
      }
      http {
        rules {
          selector: "Bookstore.ListShelves"
          get: "/shelves"
        }
        rules {
          selector: "Bookstore.ListBooks"
          get: "/shelves/{shelf=*}/books"
        }
        rules {
          selector: "Bookstore.CreateBook"
          post: "/shelves/{shelf=*}/books"
          body: "book"
        }
        rules {
          selector: "Bookstore.CreateBook"
          post: "/shelves/{shelf=*}/books/{book.id}/{book.author}"
          body: "book.title"
        }
      }
      system_parameters {
        rules {
          selector: "Bookstore.CreateBook"
          parameters {
            name: "system_paramter"
            url_query_parameter: "sys"
          }
          parameters {
            name: "system_paramter"
            url_query_parameter: "system"
          }
        }
      }
    )";

  std::unique_ptr<Config> config = Config::Create(&env, config_text, "");
  ASSERT_TRUE(config);

  MethodCallInfo list_shelves =
      config->GetMethodCallInfo("GET", "/shelves", "");

  ASSERT_NE(nullptr, list_shelves.method_info);
  EXPECT_EQ("/Bookstore/ListShelves",
            list_shelves.method_info->rpc_method_full_name());
  EXPECT_EQ("types.googleapis.com/google.protobuf.Empty",
            list_shelves.method_info->request_type_url());
  EXPECT_EQ(false, list_shelves.method_info->request_streaming());
  EXPECT_EQ("types.googleapis.com/Bookstore.ListShelvesResponse",
            list_shelves.method_info->response_type_url());
  EXPECT_EQ(false, list_shelves.method_info->response_streaming());
  EXPECT_EQ("", list_shelves.body_field_path);
  EXPECT_EQ(0, list_shelves.variable_bindings.size());

  MethodCallInfo list_books =
      config->GetMethodCallInfo("GET", "/shelves/88/books", "");

  ASSERT_NE(nullptr, list_books.method_info);
  EXPECT_EQ("/Bookstore/ListBooks",
            list_books.method_info->rpc_method_full_name());
  EXPECT_EQ("types.googleapis.com/google.protobuf.Empty",
            list_books.method_info->request_type_url());
  EXPECT_EQ(false, list_books.method_info->request_streaming());
  EXPECT_EQ("types.googleapis.com/Bookstore.ListBooksResponse",
            list_books.method_info->response_type_url());
  EXPECT_EQ(false, list_books.method_info->response_streaming());
  EXPECT_EQ("", list_books.body_field_path);
  ASSERT_EQ(1, list_books.variable_bindings.size());
  EXPECT_EQ(std::vector<std::string>(1, "shelf"),
            list_books.variable_bindings[0].field_path);
  EXPECT_EQ("88", list_books.variable_bindings[0].value);

  MethodCallInfo create_book =
      config->GetMethodCallInfo("POST", "/shelves/99/books", "");

  ASSERT_NE(nullptr, create_book.method_info);
  EXPECT_EQ("/Bookstore/CreateBook",
            create_book.method_info->rpc_method_full_name());
  EXPECT_EQ("types.googleapis.com/Bookstore.CreateBookRequest",
            create_book.method_info->request_type_url());
  EXPECT_EQ(false, create_book.method_info->request_streaming());
  EXPECT_EQ("types.googleapis.com/Bookstore.Book",
            create_book.method_info->response_type_url());
  EXPECT_EQ(false, create_book.method_info->response_streaming());
  EXPECT_EQ("book", create_book.body_field_path);
  ASSERT_EQ(1, create_book.variable_bindings.size());
  EXPECT_EQ(std::vector<std::string>(1, "shelf"),
            create_book.variable_bindings[0].field_path);
  EXPECT_EQ("99", create_book.variable_bindings[0].value);

  MethodCallInfo create_book_1 =
      config->GetMethodCallInfo("POST", "/shelves/77/books/88/auth", "");

  ASSERT_NE(nullptr, create_book_1.method_info);
  EXPECT_EQ("/Bookstore/CreateBook",
            create_book_1.method_info->rpc_method_full_name());
  EXPECT_EQ("types.googleapis.com/Bookstore.CreateBookRequest",
            create_book_1.method_info->request_type_url());
  EXPECT_EQ(false, create_book_1.method_info->request_streaming());
  EXPECT_EQ("types.googleapis.com/Bookstore.Book",
            create_book_1.method_info->response_type_url());
  EXPECT_EQ(false, create_book_1.method_info->response_streaming());
  EXPECT_EQ("book.title", create_book_1.body_field_path);
  ASSERT_EQ(3, create_book_1.variable_bindings.size());

  EXPECT_EQ(std::vector<std::string>(1, "shelf"),
            create_book_1.variable_bindings[0].field_path);
  EXPECT_EQ("77", create_book_1.variable_bindings[0].value);

  EXPECT_EQ((std::vector<std::string>{"book", "id"}),
            create_book_1.variable_bindings[1].field_path);
  EXPECT_EQ("88", create_book_1.variable_bindings[1].value);

  EXPECT_EQ((std::vector<std::string>{"book", "author"}),
            create_book_1.variable_bindings[2].field_path);
  EXPECT_EQ("auth", create_book_1.variable_bindings[2].value);

  MethodCallInfo create_book_2 = config->GetMethodCallInfo(
      "POST", "/shelves/55/books", "book.title=Readme");

  ASSERT_NE(nullptr, create_book_2.method_info);
  EXPECT_EQ("/Bookstore/CreateBook",
            create_book_2.method_info->rpc_method_full_name());
  EXPECT_EQ("types.googleapis.com/Bookstore.CreateBookRequest",
            create_book_2.method_info->request_type_url());
  EXPECT_EQ(false, create_book_2.method_info->request_streaming());
  EXPECT_EQ("types.googleapis.com/Bookstore.Book",
            create_book_2.method_info->response_type_url());
  EXPECT_EQ(false, create_book_2.method_info->response_streaming());
  EXPECT_EQ("book", create_book_2.body_field_path);
  ASSERT_EQ(2, create_book_2.variable_bindings.size());
  EXPECT_EQ(std::vector<std::string>(1, "shelf"),
            create_book_2.variable_bindings[0].field_path);
  EXPECT_EQ("55", create_book_2.variable_bindings[0].value);
  EXPECT_EQ((std::vector<std::string>{"book", "title"}),
            create_book_2.variable_bindings[1].field_path);
  EXPECT_EQ("Readme", create_book_2.variable_bindings[1].value);

  MethodCallInfo create_book_3 =
      config->GetMethodCallInfo("POST", "/shelves/321/books",
                                "book.id=123&key=0&api_key=1&sys=2&system=3");

  ASSERT_NE(nullptr, create_book_3.method_info);
  EXPECT_EQ("/Bookstore/CreateBook",
            create_book_3.method_info->rpc_method_full_name());
  EXPECT_EQ("types.googleapis.com/Bookstore.CreateBookRequest",
            create_book_3.method_info->request_type_url());
  EXPECT_EQ(false, create_book_3.method_info->request_streaming());
  EXPECT_EQ("types.googleapis.com/Bookstore.Book",
            create_book_3.method_info->response_type_url());
  EXPECT_EQ(false, create_book_3.method_info->response_streaming());
  EXPECT_EQ("book", create_book_3.body_field_path);
  ASSERT_EQ(2, create_book_3.variable_bindings.size());
  EXPECT_EQ(std::vector<std::string>(1, "shelf"),
            create_book_3.variable_bindings[0].field_path);
  EXPECT_EQ("321", create_book_3.variable_bindings[0].value);
  EXPECT_EQ((std::vector<std::string>{"book", "id"}),
            create_book_3.variable_bindings[1].field_path);
  EXPECT_EQ("123", create_book_3.variable_bindings[1].value);
}

TEST(Config, TestHttpOptions) {
  MockApiManagerEnvironmentWithLog env;

  static const char config_text[] = R"(
 name: "Service.Name"
 endpoints {
   name: "Service.Name"
   allow_cors: true
 }
 http {
   rules {
     selector: "ListShelves"
     get: "/shelves"
   }
   rules {
     selector: "CorsShelves"
     custom: {
       kind: "OPTIONS"
       path: "/shelves"
     }
   }
   rules {
     selector: "CreateShelf"
     post: "/shelves"
   }
   rules {
     selector: "GetShelf"
     get: "/shelves/{shelf}"
   }
   rules {
     selector: "DeleteShelf"
     delete: "/shelves/{shelf}"
   }
   rules {
     selector: "GetShelfBook"
     get: "/shelves/{shelf}/books"
   }
 }
)";

  std::unique_ptr<Config> config = Config::Create(&env, config_text, "");
  ASSERT_TRUE(config);

  // The one from service config.
  auto method1 = config->GetMethodInfo("OPTIONS", "/shelves");
  ASSERT_NE(nullptr, method1);
  ASSERT_EQ("CorsShelves", method1->name());
  ASSERT_FALSE(method1->auth());
  // For all service config specified method, default is NOT to allow
  // unregistered calls.
  ASSERT_FALSE(method1->allow_unregistered_calls());

  // added by the code.
  for (auto path : {"/shelves/{shelf}", "/shelves/{shelf}/books"}) {
    auto method = config->GetMethodInfo("OPTIONS", path);
    ASSERT_NE(nullptr, method);
    ASSERT_EQ("CORS", method->name());
    ASSERT_FALSE(method->auth());
    // For all added OPTIONS methods, allow_unregistered_calls is true.
    ASSERT_TRUE(method->allow_unregistered_calls());
  }

  // not registered path.
  auto method2 = config->GetMethodInfo("OPTIONS", "/xyz");
  ASSERT_EQ(nullptr, method2);
}

TEST(Config, TestHttpOptionsSelector) {
  MockApiManagerEnvironmentWithLog env;

  static const char config_text[] = R"(
 name: "Service.Name"
 endpoints {
   name: "Service.Name"
   allow_cors: true
 }
 http {
   rules {
     selector: "CORS"
     get: "/shelves"
   }
   rules {
     selector: "CORS.1"
     get: "/shelves/{shelf}"
   }
 }
)";

  std::unique_ptr<Config> config = Config::Create(&env, config_text, "");
  ASSERT_TRUE(config);

  auto method1 = config->GetMethodInfo("OPTIONS", "/shelves");
  ASSERT_NE(nullptr, method1);
  // selector for options should be appended with suffix.
  ASSERT_EQ("CORS.2", method1->name());
  ASSERT_FALSE(method1->auth());
  ASSERT_TRUE(method1->allow_unregistered_calls());
}

TEST(Config, TestCorsDisabled) {
  MockApiManagerEnvironmentWithLog env;

  static const char config_text[] = R"(
 name: "Service.Name"
 http {
   rules {
     selector: "CORS"
     get: "/shelves"
   }
   rules {
     selector: "CORS.1"
     get: "/shelves/{shelf}"
   }
 }
)";

  std::unique_ptr<Config> config = Config::Create(&env, config_text, "");
  ASSERT_TRUE(config);

  auto method1 = config->GetMethodInfo("OPTIONS", "/shelves");
  ASSERT_EQ(nullptr, method1);
}

static const char kServiceConfigWithoutAuthz[] = R"(
  name: "Service.Name"
)";

static const char kServiceConfigWithAuthz[] = R"(
  name: "Service.Name"
  experimental {
    authorization {
      provider: "authz@firebase.com"
    }
  }
)";

static const char kServerConfigWithoutAuthz[] = R"(
  service_control_config {
    check_aggregator_config {
      cache_entries: 1000
      flush_interval_ms: 10
      response_expiration_ms: 20
    }
    report_aggregator_config {
      cache_entries: 1020
      flush_interval_ms: 15
    }
  }
)";

static const char kServerConfigWithAuthz[] = R"(
  api_check_security_rules_config {
    firebase_server: "https://myfirebaseserver.com"
  }
)";

TEST(Config, TestFirebaseServerCheckWithServiceAuthzWithoutServerAuthz) {
  MockApiManagerEnvironmentWithLog env;

  std::unique_ptr<Config> config =
      Config::Create(&env, kServiceConfigWithAuthz, kServerConfigWithoutAuthz);
  ASSERT_TRUE(config);

  ASSERT_EQ(config->GetFirebaseServer(), "authz@firebase.com");
}

TEST(Config, TestFirebaseServerCheckWithServiceAuthzWithServerAuthz) {
  MockApiManagerEnvironmentWithLog env;

  std::unique_ptr<Config> config =
      Config::Create(&env, kServiceConfigWithAuthz, kServerConfigWithAuthz);
  ASSERT_TRUE(config);

  ASSERT_EQ(config->GetFirebaseServer(), "https://myfirebaseserver.com");
}

TEST(Config, TestFirebaseServerCheckWithoutServiceAuthzWithoutServerAuthz) {
  MockApiManagerEnvironmentWithLog env;

  std::unique_ptr<Config> config = Config::Create(
      &env, kServiceConfigWithoutAuthz, kServerConfigWithoutAuthz);
  ASSERT_TRUE(config);

  ASSERT_EQ(config->GetFirebaseServer(), "");
}

TEST(Config, TestFirebaseServerCheckWithoutServiceConfigWithServerConfig) {
  MockApiManagerEnvironmentWithLog env;

  std::unique_ptr<Config> config =
      Config::Create(&env, kServiceConfigWithoutAuthz, kServerConfigWithAuthz);
  ASSERT_TRUE(config);

  ASSERT_EQ(config->GetFirebaseServer(), "https://myfirebaseserver.com");
}

TEST(Config, TestFirebaseServerAudience) {
  MockApiManagerEnvironmentWithLog env;
  std::unique_ptr<Config> config =
      Config::Create(&env, kServiceConfigWithAuthz, kServerConfigWithAuthz);
  ASSERT_TRUE(config);

  ASSERT_EQ(config->GetFirebaseAudience(),
            "https://myfirebaseserver.com"
            "/google.firebase.rules.v1.FirebaseRulesService");
}

TEST(Config, TestInvalidMetricRules) {
  MockApiManagerEnvironmentWithLog env;
  // There is no http.rule or api.method to match the selector.
  static const char config_text[] = R"(
name: "Service.Name"
quota {
   metric_rules {
      selector: "GetShelves"
      metric_costs {
         key: "test.googleapis.com/operation/read_book"
         value: 100
      }
   }
}
)";

  std::unique_ptr<Config> config = Config::Create(&env, config_text, "");
  EXPECT_EQ(nullptr, config);
}

TEST(Config, TestMetricRules) {
  MockApiManagerEnvironmentWithLog env;
  static const char config_text[] = R"(
name: "Service.Name"
http {
  rules {
   selector: "DeleteShelf"
   delete: "/shelves"
  }
  rules {
   selector: "GetShelves"
   get: "/shelves"
  }
}
quota {
   metric_rules {
      selector: "GetShelves"
      metric_costs {
         key: "test.googleapis.com/operation/get_shelves"
         value: 100
      }
      metric_costs {
          key: "test.googleapis.com/operation/request"
          value: 10
       }
   }
   metric_rules {
      selector: "DeleteShelf"
      metric_costs {
         key: "test.googleapis.com/operation/delete_shelves"
         value: 200
      }
   }
}
)";

  std::unique_ptr<Config> config = Config::Create(&env, config_text, "");
  ASSERT_TRUE(config);

  const MethodInfo *method1 = config->GetMethodInfo("GET", "/shelves");
  ASSERT_NE(nullptr, method1);

  std::vector<std::pair<std::string, int>> metric_cost_vector =
      method1->metric_cost_vector();
  std::sort(metric_cost_vector.begin(), metric_cost_vector.end());
  ASSERT_EQ(2, metric_cost_vector.size());
  ASSERT_EQ("test.googleapis.com/operation/get_shelves",
            metric_cost_vector[0].first);
  ASSERT_EQ(100, metric_cost_vector[0].second);

  ASSERT_EQ("test.googleapis.com/operation/request",
            metric_cost_vector[1].first);
  ASSERT_EQ(10, metric_cost_vector[1].second);

  const MethodInfo *method2 = config->GetMethodInfo("DELETE", "/shelves");
  ASSERT_NE(nullptr, method1);

  metric_cost_vector = method2->metric_cost_vector();
  ASSERT_EQ(1, metric_cost_vector.size());
  ASSERT_EQ("test.googleapis.com/operation/delete_shelves",
            metric_cost_vector[0].first);
  ASSERT_EQ(200, metric_cost_vector[0].second);
}

}  // namespace

}  // namespace api_manager
}  // namespace google
