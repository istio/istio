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
#include "src/grpc/transcoding/request_message_translator.h"

#include <memory>
#include <string>

#include "google/protobuf/struct.pb.h"
#include "google/protobuf/type.pb.h"
#include "gtest/gtest.h"
#include "src/grpc/transcoding/bookstore.pb.h"
#include "src/grpc/transcoding/request_translator_test_base.h"
#include "src/grpc/transcoding/test_common.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {
namespace {

class RequestMessageTranslatorTest : public RequestTranslatorTestBase {
 protected:
  RequestMessageTranslatorTest() : RequestTranslatorTestBase() {}

  template <typename MessageType>
  bool ExpectMessageEq(const std::string& expected_proto_text) {
    // We expect only one message
    return Tester().ExpectFinishedEq(false) &&
           Tester().ExpectNextEq<MessageType>(expected_proto_text) &&
           Tester().ExpectFinishedEq(true);
  }

  google::protobuf::util::converter::ObjectWriter& Input() {
    return translator_->Input();
  }

 private:
  // RequestTranslatorTestBase::Create()
  virtual MessageStream* Create(
      google::protobuf::util::TypeResolver& type_resolver,
      bool output_delimiters, RequestInfo request_info) {
    translator_.reset(new RequestMessageTranslator(
        type_resolver, output_delimiters, std::move(request_info)));
    return translator_.get();
  }

  std::unique_ptr<RequestMessageTranslator> translator_;
};

TEST_F(RequestMessageTranslatorTest, Simple) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input()
      .StartObject("")
      ->RenderString("name", "1")
      ->RenderString("theme", "History")
      ->EndObject();

  auto expected = R"(
    name : "1"
    theme : "History"
  )";

  EXPECT_TRUE(ExpectMessageEq<Shelf>(expected));
}

TEST_F(RequestMessageTranslatorTest, Nested) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateShelfRequest");
  Build();
  Input()
      .StartObject("")
      ->StartObject("shelf")
      ->RenderString("name", "2")
      ->RenderString("theme", "Russian")
      ->EndObject()
      ->EndObject();

  auto expected = R"(
    shelf : {
      name : "2"
      theme : "Russian"
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateShelfRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, MultipleLevelNested) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  Build();
  Input()
      .StartObject("")
      ->RenderString("shelf", "99")
      ->StartObject("book")
      ->RenderString("name", "999")
      ->RenderString("author", "Leo Tolstoy")
      ->RenderString("title", "War and Peace")
      ->StartObject("authorInfo")
      ->RenderString("firstName", "Leo")
      ->RenderString("lastName", "Tolstoy")
      ->StartObject("bio")
      ->RenderString("yearBorn", "1830")
      ->RenderString("yearDied", "1910")
      ->RenderString("text", "bio text")
      ->EndObject()   // bio
      ->EndObject()   // authorInfo
      ->EndObject()   // book
      ->EndObject();  // ""

  auto expected = R"(
    shelf : 99
    book {
      name : "999"
      author : "Leo Tolstoy"
      title : "War and Peace"
      author_info {
        first_name : "Leo"
        last_name : "Tolstoy"
        bio {
          year_born : 1830
          year_died : 1910
          text : "bio text"
        }
      }
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, Empty) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input().StartObject("")->EndObject();

  EXPECT_TRUE(ExpectMessageEq<Shelf>(""));
}

TEST_F(RequestMessageTranslatorTest, Delimiter) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetOutputDelimiters(true);
  Build();
  Input()
      .StartObject("")
      ->RenderString("shelf", "7")
      ->StartObject("book")
      ->RenderString("name", "77")
      ->RenderString("author", "Leo Tolstoy")
      ->RenderString("title", "Anna Karenina")
      ->EndObject()   // book
      ->EndObject();  // ""

  auto expected = R"(
    shelf : 7
    book {
      name : "77"
      author : "Leo Tolstoy"
      title : "Anna Karenina"
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, DelimiterDifferentSizes) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetOutputDelimiters(true);

  auto sizes = {1, 256, 1024, 1234, 4096, 65537};
  for (auto size : sizes) {
    Build();

    auto title = GenerateInput("0123456789abcdefgh", size);
    Input()
        .StartObject("")
        ->RenderString("shelf", "7")
        ->StartObject("book")
        ->RenderString("name", "77")
        ->RenderString("author", "Leo Tolstoy")
        ->RenderString("title", title)
        ->EndObject()   // book
        ->EndObject();  // ""

    auto expected = R"(
      shelf : 7
      book {
        name : "77"
        author : "Leo Tolstoy"
        title : ")" +
                    title + R"("
      })";

    EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(expected))
        << "Delimiter test failed for size " << size << std::endl;
  }
}

TEST_F(RequestMessageTranslatorTest, DelimiterEmpty) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetOutputDelimiters(true);
  Build();
  Input().StartObject("")->EndObject();  // ""

  EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(""));
}

TEST_F(RequestMessageTranslatorTest, Bindings) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  AddVariableBinding("shelf", "99");
  AddVariableBinding("book.author", "Leo Tolstoy");
  AddVariableBinding("book.authorInfo.firstName", "Leo");
  AddVariableBinding("book.authorInfo.lastName", "Tolstoy");
  Build();
  Input()
      .StartObject("")
      ->StartObject("book")
      ->RenderString("name", "999")
      ->RenderString("title", "War and Peace")
      // authorInfo {
      //   first_name : "Leo" <-- weaved
      //   last_name : "Tolstoy" <-- weaved
      // }
      // author : "Leo Tolstoy" <-- weaved
      ->EndObject()  // book
      // weaved: shelf : 99 <-- weaved
      ->EndObject();  // ""

  auto expected = R"(
    shelf : 99
    book {
      name : "999"
      author : "Leo Tolstoy"
      title : "War and Peace"
      author_info {
        first_name : "Leo"
        last_name : "Tolstoy"
      }
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, Prefix) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetBodyPrefix("book");
  Build();
  Input()
      .StartObject("")
      // book { <-- prefix
      ->RenderString("name", "777")
      ->RenderString("author", "Leo Tolstoy")
      ->RenderString("title", "War and Peace")
      // } <-- end of prefix
      ->EndObject();  // ""

  auto expected = R"(
    book {
      name : "777"
      author : "Leo Tolstoy"
      title : "War and Peace"
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, NestedPrefix) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetBodyPrefix("book.authorInfo.bio");
  Build();
  Input()
      .StartObject("")
      // book { authorInfo { bio { <-- prefix
      ->RenderString("yearBorn", "1830")
      ->RenderString("yearDied", "1910")
      ->RenderString("text", "bio text")
      // }}} <-- end of prefix
      ->EndObject();  // ""

  auto expected = R"(
    book {
      author_info {
        bio {
          year_born : 1830
          year_died : 1910
          text : "bio text"
        }
      }
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, PrefixAndBinding) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetBodyPrefix("book");
  AddVariableBinding("shelf", "99");
  SetOutputDelimiters(true);
  Build();
  Input()
      .StartObject("")
      // book { <-- prefix
      ->RenderString("name", "999")
      ->RenderString("author", "Leo Tolstoy")
      ->RenderString("title", "War and Peace")
      // } <-- end of prefix
      // shelf : 99 <-- weaved
      ->EndObject();  // ""

  auto expected = R"(
    shelf : 99
    book {
      name : "999"
      author : "Leo Tolstoy"
      title : "War and Peace"
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, ScalarBody) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateShelfRequest");
  SetBodyPrefix("shelf.theme");
  Build();
  Input().RenderString("", "History");

  auto expected = R"(
    shelf {
      theme : "History"
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateShelfRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, ListBody) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("ListShelvesResponse");
  SetBodyPrefix("shelves");
  Build();
  Input()
      .StartList("")
      ->StartObject("")
      ->RenderString("name", "1")
      ->RenderString("theme", "History")
      ->EndObject()  // ""
      ->StartObject("")
      ->RenderString("name", "2")
      ->RenderString("theme", "Mystery")
      ->EndObject()  // ""
      ->EndList();   // ""

  auto expected = R"(
    shelves {
      name : "1"
      theme : "History"
    }
    shelves {
      name : "2"
      theme : "Mystery"
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<ListShelvesResponse>(expected));
}

TEST_F(RequestMessageTranslatorTest, PartialObject) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  Build();
  EXPECT_EQ(true, Tester().ExpectNone());

  Input().StartObject("");
  EXPECT_EQ(true, Tester().ExpectNone());

  Input().RenderString("shelf", "99");
  EXPECT_EQ(true, Tester().ExpectNone());

  Input()
      .StartObject("book")
      ->RenderString("name", "999")
      ->RenderString("author", "Leo Tolstoy")
      ->RenderString("title", "War and Peace");
  EXPECT_EQ(true, Tester().ExpectNone());

  Input()
      .StartObject("authorInfo")
      ->RenderString("firstName", "Leo")
      ->RenderString("lastName", "Tolstoy")
      ->EndObject();  // authorInfo
  EXPECT_EQ(true, Tester().ExpectNone());

  Input()
      .EndObject()    // book
      ->EndObject();  // ""

  auto expected = R"(
    shelf : 99
    book {
      name : "999"
      author : "Leo Tolstoy"
      title : "War and Peace"
      author_info {
        first_name : "Leo"
        last_name : "Tolstoy"
      }
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateBookRequest>(expected));
}

TEST_F(RequestMessageTranslatorTest, UnexpectedScalarBody) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Book");
  Build();
  Input().RenderString("", "History");

  EXPECT_TRUE(Tester().ExpectStatusEq(
      ::google::protobuf::util::error::INVALID_ARGUMENT));
}

TEST_F(RequestMessageTranslatorTest, UnexpectedList) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Book");
  Build();
  Input().StartList("")->EndList();

  EXPECT_TRUE(Tester().ExpectStatusEq(
      ::google::protobuf::util::error::INVALID_ARGUMENT));
}

TEST_F(RequestMessageTranslatorTest, IgnoreUnkownFields) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateShelfRequest");
  Build();
  Input()
      .StartObject("")
      ->StartObject("shelf")
      ->RenderString("name", "3")
      ->RenderString("theme", "Classics")
      // Unkown field
      ->RenderString("unknownField", "value")
      ->EndObject()
      // Unkown object
      ->StartObject("unknownObject")
      ->RenderString("field", "value")
      ->EndObject()
      // Unkown list
      ->StartList("unknownList")
      ->RenderString("field1", "value1")
      ->RenderString("field2", "value2")
      ->EndList()
      ->EndObject();

  auto expected = R"(
    shelf : {
      name : "3"
      theme : "Classics"
    }
  )";

  EXPECT_TRUE(ExpectMessageEq<CreateShelfRequest>(expected));
}

}  // namespace
}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
