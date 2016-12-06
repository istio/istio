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
#include "src/grpc/transcoding/request_stream_translator.h"

#include <memory>
#include <string>

#include "google/protobuf/type.pb.h"
#include "gtest/gtest.h"
#include "src/grpc/transcoding/bookstore.pb.h"
#include "src/grpc/transcoding/request_translator_test_base.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {
namespace {

class RequestStreamTranslatorTest : public RequestTranslatorTestBase {
 protected:
  RequestStreamTranslatorTest() : RequestTranslatorTestBase() {}

  google::protobuf::util::converter::ObjectWriter& Input() {
    return *translator_;
  }

 private:
  // RequestTranslatorTestBase::Create()
  virtual MessageStream* Create(
      google::protobuf::util::TypeResolver& type_resolver,
      bool output_delimiters, RequestInfo request_info) {
    translator_.reset(new RequestStreamTranslator(
        type_resolver, output_delimiters, std::move(request_info)));
    return translator_.get();
  }

  std::unique_ptr<RequestStreamTranslator> translator_;
};

TEST_F(RequestStreamTranslatorTest, One) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input()
      .StartList("")
      ->StartObject("")
      ->RenderString("name", "1")
      ->RenderString("theme", "History");
  EXPECT_TRUE(Tester().ExpectNone());

  // EndObject() should produce one message
  Input().EndObject();  // ""
  EXPECT_TRUE(Tester().ExpectNextEq<Shelf>(R"(name : "1" theme : "History")"));

  // We're not finished yet (still expecting an end of list)
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input().EndList();  // ""
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, Two) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input()
      .StartList("")
      ->StartObject("")
      ->RenderString("name", "1")
      ->RenderString("theme", "History")
      ->EndObject();  // ""

  EXPECT_TRUE(Tester().ExpectNextEq<Shelf>(R"(name : "1" theme : "History")"));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input()
      .StartObject("")
      ->RenderString("name", "2")
      ->RenderString("theme", "Mistery")
      ->EndObject();  // ""

  EXPECT_TRUE(Tester().ExpectNextEq<Shelf>(R"(name : "2" theme : "Mistery")"));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input().EndList();  // ""
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, OneHundred) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();

  Input().StartList("");
  for (int i = 0; i < 100; ++i) {
    auto n = std::to_string(i + 1);

    Input().StartObject("");
    EXPECT_TRUE(Tester().ExpectNone());

    Input().RenderString("name", n);
    EXPECT_TRUE(Tester().ExpectNone());

    Input().RenderString("theme", "Theme" + n);
    EXPECT_TRUE(Tester().ExpectNone());

    Input().EndObject();  // ""

    EXPECT_TRUE(Tester().ExpectNextEq<Shelf>("name : \"" + n +
                                             "\" theme : \"Theme" + n + "\""));
    EXPECT_TRUE(Tester().ExpectFinishedEq(false));
  }
  Input().EndList();  // ""
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, Nested) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  Build();
  Input().StartList("");

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

  auto expected1 = R"(
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

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected1));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input()
      .StartObject("")
      ->RenderString("shelf", "77")
      ->StartObject("book")
      ->RenderString("name", "777")
      ->RenderString("author", "Fyodor Dostoevski")
      ->RenderString("title", "Crime & Punishment")
      ->StartObject("authorInfo")
      ->RenderString("firstName", "Fyodor")
      ->RenderString("lastName", "Dostoevski")
      ->StartObject("bio")
      ->RenderString("yearBorn", "1850")
      ->RenderString("yearDied", "1920")
      ->RenderString("text", "bio text")
      ->EndObject()   // bio
      ->EndObject()   // authorInfo
      ->EndObject()   // book
      ->EndObject();  // ""

  auto expected2 = R"(
    shelf : 77
    book {
      name : "777"
      author : "Fyodor Dostoevski"
      title : "Crime & Punishment"
      author_info {
        first_name : "Fyodor"
        last_name : "Dostoevski"
        bio {
          year_born : 1850
          year_died : 1920
          text : "bio text"
        }
      }
    }
  )";

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected2));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input().EndList();  // ""
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, Empty) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  Build();

  Input().StartList("");
  EXPECT_TRUE(Tester().ExpectNone());
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input().EndList();  // ""
  EXPECT_TRUE(Tester().ExpectNone());
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, DelimiterEmpty) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetOutputDelimiters(true);
  Build();

  Input().StartList("");
  EXPECT_TRUE(Tester().ExpectNone());
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input().EndList();  // ""
  EXPECT_TRUE(Tester().ExpectNone());
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, Delimiter) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetOutputDelimiters(true);
  Build();
  Input().StartList("");

  Input()
      .StartObject("")
      ->RenderString("shelf", "99")
      ->StartObject("book")
      ->RenderString("name", "999")
      ->RenderString("author", "Leo Tolstoy")
      ->RenderString("title", "War and Peace")
      ->EndObject()   // book
      ->EndObject();  // ""

  auto expected1 = R"(
    shelf : 99
    book {
      name : "999"
      author : "Leo Tolstoy"
      title : "War and Peace"
    }
  )";

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected1));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input()
      .StartObject("")
      ->RenderString("shelf", "77")
      ->StartObject("book")
      ->RenderString("name", "777")
      ->RenderString("author", "Fyodor Dostoevski")
      ->RenderString("title", "Crime & Punishment")
      ->EndObject()   // book
      ->EndObject();  // ""

  auto expected2 = R"(
    shelf : 77
    book {
      name : "777"
      author : "Fyodor Dostoevski"
      title : "Crime & Punishment"
    }
  )";

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected2));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  Input().EndList();  // ""
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, Prefix) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetBodyPrefix("book.authorInfo.bio");
  Build();
  Input()
      .StartList("")
      ->StartObject("")
      // book { authorInfo { biod { <-- prefix
      ->RenderString("yearBorn", "1830")
      ->RenderString("yearDied", "1910")
      ->RenderString("text", "bio text 1")
      // } } } <-- end of prefix
      ->EndObject()  // ""
      ->StartObject("")
      // book { authorInfo { biod { <-- prefix
      ->RenderString("yearBorn", "1840")
      ->RenderString("yearDied", "1920")
      ->RenderString("text", "bio text 2")
      // } } } <-- end of prefix
      ->EndObject()  // ""
      ->EndList();   // ""

  auto expected1 = R"(
    book {
      author_info {
        bio {
          year_born : 1830
          year_died : 1910
          text : "bio text 1"
        }
      }
    }
  )";

  auto expected2 = R"(
    book {
      author_info {
        bio {
          year_born : 1840
          year_died : 1920
          text : "bio text 2"
        }
      }
    }
  )";

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected1));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected2));
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, Bindings) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  AddVariableBinding("shelf", "8");
  AddVariableBinding("book.authorInfo.firstName", "Leo");
  AddVariableBinding("book.authorInfo.lastName", "Tolstoy");
  SetBodyPrefix("book");
  Build();
  Input()
      .StartList("")
      ->StartObject("")
      // book { <-- prefix
      ->RenderString("name", "1")
      ->RenderString("author", "Leo Tolstoy")
      ->RenderString("title", "War and Peace")
      // authorInfo {
      //   first_name : "Leo" <-- weaved
      //   last_name : "Tolstoy" <-- weaved
      // }
      // } <-- prefix
      // shelf : 8 <-- weaved
      ->EndObject()  // ""
      ->StartObject("")
      // book { <-- prefix
      ->RenderString("name", "2")
      ->RenderString("author", "Leo Tolstoy")
      ->RenderString("title", "Anna Karenina")
      // }
      ->EndObject()  // ""
      ->StartObject("")
      // book { <-- prefix
      ->RenderString("name", "3")
      ->RenderString("author", "Fyodor Dostoevski")
      ->RenderString("title", "Crime and Punishment")
      // }
      ->EndObject()  // ""
      ->EndList();   // ""

  // The bindings have effect only on the first message
  auto expected1 = R"(
    shelf : 8
    book {
      name : "1"
      author : "Leo Tolstoy"
      title : "War and Peace"
      author_info {
        first_name : "Leo"
        last_name : "Tolstoy"
      }
    }
  )";

  auto expected2 = R"(
    book {
      name : "2"
      author : "Leo Tolstoy"
      title : "Anna Karenina"
    }
  )";

  auto expected3 = R"(
    book {
      name : "3"
      author : "Fyodor Dostoevski"
      title : "Crime and Punishment"
    }
  )";

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected1));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected2));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));

  EXPECT_TRUE(Tester().ExpectNextEq<CreateBookRequest>(expected3));
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, ListOfScalars) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  SetBodyPrefix("theme");
  Build();
  Input()
      .StartList("")
      ->RenderString("", "History")
      ->RenderString("", "Mistery")
      ->RenderString("", "Russian")
      ->EndList();

  EXPECT_TRUE(Tester().ExpectNextEq<Shelf>("theme : \"History\""));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));
  EXPECT_TRUE(Tester().ExpectNextEq<Shelf>("theme : \"Mistery\""));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));
  EXPECT_TRUE(Tester().ExpectNextEq<Shelf>("theme : \"Russian\""));
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, ListOfLists) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("ListShelvesResponse");
  SetBodyPrefix("shelves");
  Build();
  Input().StartList("");

  Input()
      .StartList("")
      ->StartObject("")
      ->RenderString("name", "1")
      ->RenderString("theme", "A")
      ->EndObject()
      ->StartObject("")
      ->RenderString("name", "2")
      ->RenderString("theme", "B")
      ->EndObject()
      ->EndList();

  Input()
      .StartList("")
      ->StartObject("")
      ->RenderString("name", "3")
      ->RenderString("theme", "C")
      ->EndObject()
      ->StartObject("")
      ->RenderString("name", "4")
      ->RenderString("theme", "D")
      ->EndObject()
      ->EndList();

  Input().EndList();

  auto expected1 = R"(
    shelves {
      name : "1"
      theme : "A"
    }
    shelves {
      name : "2"
      theme : "B"
    }
  )";

  auto expected2 = R"(
    shelves {
      name : "3"
      theme : "C"
    }
    shelves {
      name : "4"
      theme : "D"
    }
  )";

  EXPECT_TRUE(Tester().ExpectNextEq<ListShelvesResponse>(expected1));
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));
  EXPECT_TRUE(Tester().ExpectNextEq<ListShelvesResponse>(expected2));
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(RequestStreamTranslatorTest, Error1) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input().StartObject("");
  Tester().ExpectStatusEq(google::protobuf::util::error::INVALID_ARGUMENT);
}

TEST_F(RequestStreamTranslatorTest, Error2) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input().StartList("");
  Input().EndObject();
  Tester().ExpectStatusEq(google::protobuf::util::error::INVALID_ARGUMENT);
}

TEST_F(RequestStreamTranslatorTest, Error3) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input().EndList();
  Tester().ExpectStatusEq(google::protobuf::util::error::INVALID_ARGUMENT);
}

TEST_F(RequestStreamTranslatorTest, Error4) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input().EndObject();
  Tester().ExpectStatusEq(google::protobuf::util::error::INVALID_ARGUMENT);
}

TEST_F(RequestStreamTranslatorTest, Error5) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input().StartList("");
  Input().StartList("");
  Input().EndObject();
  Tester().ExpectStatusEq(google::protobuf::util::error::INVALID_ARGUMENT);
}

TEST_F(RequestStreamTranslatorTest, Error6) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Input().StartList("");
  Input().StartObject("");
  Input().RenderString("theme", "Russian");
  Input().EndList();
  Input().EndList();
  // google::protobuf::ProtoStreamObjectWriter for some reason accepts EndList()
  // instead of EndObject(). Should be an error instead.
  // Tester().ExpectStatusEq(google::protobuf::util::error::INVALID_ARGUMENT);
  Tester().ExpectNextEq<Shelf>(R"( theme : "Russian" )");
}

}  // namespace
}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
