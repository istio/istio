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
#include "contrib/endpoints/src/grpc/transcoding/type_helper.h"

#include <fstream>
#include <memory>
#include <sstream>
#include <vector>

#include "contrib/endpoints/src/grpc/transcoding/test_common.h"
#include "google/api/service.pb.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/type.pb.h"
#include "gtest/gtest.h"

namespace pb = ::google::protobuf;

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {

namespace {

class TypeHelperTest : public ::testing::Test {
 protected:
  TypeHelperTest() : types_(), enums_() {}

  void AddType(const std::string& n) {
    pb::Type t;
    t.set_name(n);
    types_.emplace_back(std::move(t));
  }

  void AddEnum(const std::string& n) {
    pb::Enum e;
    e.set_name(n);
    enums_.emplace_back(std::move(e));
  }

  void Build() { helper_.reset(new TypeHelper(types_, enums_)); }

  const pb::Type* GetType(const std::string& url) {
    return helper_->Info()->GetTypeByTypeUrl(url);
  }

  const pb::Enum* GetEnum(const std::string& url) {
    return helper_->Info()->GetEnumByTypeUrl(url);
  }

 private:
  std::vector<pb::Type> types_;
  std::vector<pb::Enum> enums_;
  std::unique_ptr<TypeHelper> helper_;
};

TEST_F(TypeHelperTest, OneType) {
  AddType("Shelf");
  Build();

  ASSERT_NE(nullptr, GetType("type.googleapis.com/Shelf"));
  EXPECT_EQ("Shelf", GetType("type.googleapis.com/Shelf")->name());
  EXPECT_EQ(nullptr, GetEnum("type.googleapis.com/Shelf"));
}

TEST_F(TypeHelperTest, OneEnum) {
  AddEnum("ShelfType");
  Build();

  ASSERT_NE(nullptr, GetEnum("type.googleapis.com/ShelfType"));
  EXPECT_EQ("ShelfType", GetEnum("type.googleapis.com/ShelfType")->name());
  EXPECT_EQ(nullptr, GetType("type.googleapis.com/ShelfType"));
}

TEST_F(TypeHelperTest, MultipleTypesAndEnums) {
  AddType("Shelf");
  AddType("ShelfList");
  AddType("Book");
  AddType("BookList");
  AddEnum("ShelfType");
  AddEnum("BookType");
  Build();

  ASSERT_NE(nullptr, GetType("type.googleapis.com/Shelf"));
  EXPECT_EQ("Shelf", GetType("type.googleapis.com/Shelf")->name());

  ASSERT_NE(nullptr, GetType("type.googleapis.com/ShelfList"));
  EXPECT_EQ("ShelfList", GetType("type.googleapis.com/ShelfList")->name());

  ASSERT_NE(nullptr, GetType("type.googleapis.com/Book"));
  EXPECT_EQ("Book", GetType("type.googleapis.com/Book")->name());

  ASSERT_NE(nullptr, GetType("type.googleapis.com/BookList"));
  EXPECT_EQ("BookList", GetType("type.googleapis.com/BookList")->name());

  ASSERT_NE(nullptr, GetEnum("type.googleapis.com/ShelfType"));
  EXPECT_EQ("ShelfType", GetEnum("type.googleapis.com/ShelfType")->name());

  ASSERT_NE(nullptr, GetEnum("type.googleapis.com/BookType"));
  EXPECT_EQ("BookType", GetEnum("type.googleapis.com/BookType")->name());
}

TEST_F(TypeHelperTest, UrlMismatch) {
  AddType("Shelf");
  AddEnum("ShelfType");
  Build();

  EXPECT_EQ(nullptr, GetType("type.other.com/Shelf"));
  EXPECT_EQ(nullptr, GetEnum("type.other.com/ShelfType"));
}

class ServiceConfigBasedTypeHelperTest : public ::testing::Test {
 protected:
  ServiceConfigBasedTypeHelperTest() {}

  bool LoadService(const std::string& config_pb_txt) {
    if (!transcoding::testing::LoadService(config_pb_txt, &service_)) {
      return false;
    }
    helper_.reset(new TypeHelper(service_.types(), service_.enums()));
    return true;
  }

  const pb::Type* GetType(const std::string& url) {
    return helper_->Info()->GetTypeByTypeUrl(url);
  }

  const pb::Enum* GetEnum(const std::string& url) {
    return helper_->Info()->GetEnumByTypeUrl(url);
  }

  const pb::Field* GetField(const std::string& type_url,
                            const std::string& field_name) {
    auto t = GetType(type_url);
    if (nullptr == t) {
      return nullptr;
    }
    return helper_->Info()->FindField(t, field_name);
  }

  bool ResolveFieldPath(const std::string& type_name,
                        const std::string& field_path_str,
                        std::vector<const pb::Field*>* field_path) {
    auto type = GetType("type.googleapis.com/" + type_name);
    if (nullptr == type) {
      ADD_FAILURE() << "Could not find top level type \"" + type_name + "\""
                    << std::endl;
      return false;
    }

    auto status = helper_->ResolveFieldPath(*type, field_path_str, field_path);
    if (!status.ok()) {
      ADD_FAILURE() << "Error " << status.error_code() << " - "
                    << status.error_message() << std::endl;
      return false;
    }

    return true;
  }

 private:
  ::google::api::Service service_;
  std::unique_ptr<TypeHelper> helper_;
};

TEST_F(ServiceConfigBasedTypeHelperTest, FullTypeTests) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));

  auto t = GetType("type.googleapis.com/CreateShelfRequest");

  ASSERT_NE(nullptr, t);
  EXPECT_EQ("CreateShelfRequest", t->name());
  EXPECT_EQ(1, t->fields_size());
  EXPECT_EQ(pb::Field::TYPE_MESSAGE, t->fields(0).kind());
  EXPECT_EQ(pb::Field::CARDINALITY_OPTIONAL, t->fields(0).cardinality());
  EXPECT_EQ(1, t->fields(0).number());
  EXPECT_EQ("shelf", t->fields(0).name());
  EXPECT_EQ("type.googleapis.com/Shelf", t->fields(0).type_url());

  t = GetType("type.googleapis.com/Shelf");

  ASSERT_NE(nullptr, t);
  EXPECT_EQ("Shelf", t->name());
  EXPECT_EQ(2, t->fields_size());
  EXPECT_EQ(pb::Field::TYPE_STRING, t->fields(0).kind());
  EXPECT_EQ(pb::Field::CARDINALITY_OPTIONAL, t->fields(0).cardinality());
  EXPECT_EQ(1, t->fields(0).number());
  EXPECT_EQ("name", t->fields(0).name());
  EXPECT_EQ(pb::Field::TYPE_STRING, t->fields(1).kind());
  EXPECT_EQ(pb::Field::CARDINALITY_OPTIONAL, t->fields(1).cardinality());
  EXPECT_EQ(2, t->fields(1).number());
  EXPECT_EQ("theme", t->fields(1).name());
}

TEST_F(ServiceConfigBasedTypeHelperTest, AllTypesTests) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));

  ASSERT_NE(nullptr, GetType("type.googleapis.com/CreateShelfRequest"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/ListShelvesResponse"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/CreateBookRequest"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/ListBooksResponse"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/GetBookRequest"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/DeleteBookRequest"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/Shelf"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/Book"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/AuthorInfo"));
  ASSERT_NE(nullptr, GetType("type.googleapis.com/Biography"));

  EXPECT_EQ("CreateShelfRequest",
            GetType("type.googleapis.com/CreateShelfRequest")->name());
  EXPECT_EQ("ListShelvesResponse",
            GetType("type.googleapis.com/ListShelvesResponse")->name());
  EXPECT_EQ("CreateBookRequest",
            GetType("type.googleapis.com/CreateBookRequest")->name());
  EXPECT_EQ("ListBooksResponse",
            GetType("type.googleapis.com/ListBooksResponse")->name());
  EXPECT_EQ("GetBookRequest",
            GetType("type.googleapis.com/GetBookRequest")->name());
  EXPECT_EQ("DeleteBookRequest",
            GetType("type.googleapis.com/DeleteBookRequest")->name());
  EXPECT_EQ("Shelf", GetType("type.googleapis.com/Shelf")->name());
  EXPECT_EQ("Book", GetType("type.googleapis.com/Book")->name());
  EXPECT_EQ("AuthorInfo", GetType("type.googleapis.com/AuthorInfo")->name());
  EXPECT_EQ("Biography", GetType("type.googleapis.com/Biography")->name());

  EXPECT_EQ(nullptr, GetType("type.googleapis.com/DoesNotExist"));
  EXPECT_EQ(nullptr, GetType("type.other.com/Shelf"));
}

TEST_F(ServiceConfigBasedTypeHelperTest, FindFieldTests) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));

  auto f = GetField("type.googleapis.com/CreateShelfRequest", "shelf");
  ASSERT_NE(nullptr, f);
  EXPECT_EQ("shelf", f->name());
  EXPECT_EQ(1, f->number());

  f = GetField("type.googleapis.com/Shelf", "theme");
  ASSERT_NE(nullptr, f);
  EXPECT_EQ("theme", f->name());
  EXPECT_EQ(2, f->number());

  f = GetField("type.googleapis.com/GetBookRequest", "shelf");
  ASSERT_NE(nullptr, f);
  EXPECT_EQ("shelf", f->name());
  EXPECT_EQ(1, f->number());

  f = GetField("type.googleapis.com/GetBookRequest", "book");
  ASSERT_NE(nullptr, f);
  EXPECT_EQ("book", f->name());
  EXPECT_EQ(2, f->number());
}

TEST_F(ServiceConfigBasedTypeHelperTest, FindFieldCamelCaseTests) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));

  auto f = GetField("type.googleapis.com/AuthorInfo", "firstName");
  ASSERT_NE(nullptr, f);
  EXPECT_EQ("first_name", f->name());
  EXPECT_EQ(1, f->number());

  f = GetField("type.googleapis.com/AuthorInfo", "first_name");
  ASSERT_NE(nullptr, f);
  EXPECT_EQ("first_name", f->name());
  EXPECT_EQ(1, f->number());

  f = GetField("type.googleapis.com/AuthorInfo", "lastName");
  ASSERT_NE(nullptr, f);
  EXPECT_EQ("last_name", f->name());
  EXPECT_EQ(2, f->number());

  f = GetField("type.googleapis.com/AuthorInfo", "last_name");
  ASSERT_NE(nullptr, f);
  EXPECT_EQ("last_name", f->name());
  EXPECT_EQ(2, f->number());
}

TEST_F(ServiceConfigBasedTypeHelperTest, ResolveFieldPathTests) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));

  std::vector<const pb::Field*> field_path;

  // empty
  EXPECT_TRUE(ResolveFieldPath("Shelf", "", &field_path));
  EXPECT_TRUE(field_path.empty());

  // 1 level deep
  EXPECT_TRUE(ResolveFieldPath("CreateShelfRequest", "shelf", &field_path));
  ASSERT_EQ(1, field_path.size());
  EXPECT_EQ("shelf", field_path[0]->name());

  // 2 levels deep
  EXPECT_TRUE(
      ResolveFieldPath("CreateShelfRequest", "shelf.theme", &field_path));
  ASSERT_EQ(2, field_path.size());
  EXPECT_EQ("shelf", field_path[0]->name());
  EXPECT_EQ("theme", field_path[1]->name());

  // 2 levels deep camel-case
  EXPECT_TRUE(
      ResolveFieldPath("CreateBookRequest", "book.authorInfo", &field_path));
  ASSERT_EQ(2, field_path.size());
  EXPECT_EQ("book", field_path[0]->name());
  EXPECT_EQ("author_info", field_path[1]->name());

  // 2 levels deep non-camel-case
  EXPECT_TRUE(
      ResolveFieldPath("CreateBookRequest", "book.author_info", &field_path));
  ASSERT_EQ(2, field_path.size());
  EXPECT_EQ("book", field_path[0]->name());
  EXPECT_EQ("author_info", field_path[1]->name());

  // 4 levels deep camel case
  EXPECT_TRUE(ResolveFieldPath("CreateBookRequest",
                               "book.authorInfo.bio.yearBorn", &field_path));
  ASSERT_EQ(4, field_path.size());
  EXPECT_EQ("book", field_path[0]->name());
  EXPECT_EQ("author_info", field_path[1]->name());
  EXPECT_EQ("bio", field_path[2]->name());
  EXPECT_EQ("year_born", field_path[3]->name());
}

}  // namespace

}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
