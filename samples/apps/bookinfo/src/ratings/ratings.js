// Copyright 2017 Istio Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

var http = require('http');
var dispatcher = require('httpdispatcher');

port = parseInt(process.argv[2]);

if (process.env.SERVICE_VERSION == 'v2') {
  var MongoClient = require('mongodb').MongoClient
  var url = process.env.MONGO_DB_URL
}

var ratingsResponse = {"Reviewer1": 5, "Reviewer2": 4}

dispatcher.onGet("/", function(req, res) {
    res.writeHead(200)
    res.end(
        '<html>' +
        '<head>' +
        '<meta charset="utf-8">' +
        '<meta http-equiv="X-UA-Compatible" content="IE=edge">' +
        '<meta name="viewport" content="width=device-width, initial-scale=1">' +
        '<!-- Latest compiled and minified CSS -->' +
        '<link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/3.3.5/css/bootstrap.min.css">' +
        '<!-- Optional theme -->' +
        '<link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/3.3.5/css/bootstrap-theme.min.css">' +
        '<!-- Latest compiled and minified JavaScript -->' +
        '<script src="https://ajax.googleapis.com/ajax/libs/jquery/2.1.4/jquery.min.js"></script>' +
        '<!-- Latest compiled and minified JavaScript -->' +
        '<script src="https://maxcdn.bootstrapcdn.com/bootstrap/3.3.5/js/bootstrap.min.js"></script>' +
        '</head>' +
        '<title>Book ratings service</title>' +
        '<body>' +
        '<p><h2>Hello! This is the book ratings service. My content is</h2></p>' +
        '<div>' + JSON.stringify(ratingsResponse) + '</div>' +
        '</body>' +
        '</html>',
        {"Content-type": "text/html"})
})

dispatcher.onGet("/ratings", function(req, res) {
  if (process.env.SERVICE_VERSION == "v2") {
    var first_rating = 0
    var second_rating = 0
    MongoClient.connect(url, function (err, db) {
      if (err) {
        res.writeHead(500, {"Content-type": "application/json"})
        res.end(JSON.stringify(err))
      } else {
        db.collection('ratings').find({}).toArray(function (err, data) {
          if (err) {
            res.writeHead(500, {"Content-type": "application/json"})
            res.end(JSON.stringify(err))
          } else {
            first_rating = data[0].rating
            second_rating = data[1].rating
            var result = {
              Reviewer1: first_rating,
              Reviewer2: second_rating
            }
            res.writeHead(200, {"Content-type": "application/json"})
            res.end(JSON.stringify(result))
          }
          // close DB in any case:
          db.close()
        })
      }
    })
  }
  else {
    res.writeHead(200, {"Content-type": "application/json"})
    res.end(JSON.stringify(ratingsResponse))
  }
})

dispatcher.onGet("/health", function(req, res) {
    res.writeHead(200, {"Content-type": "text/plain"})
    res.end("Ratings is healthy")
})

function handleRequest(request, response){
    try {
        console.log(request.method + " " + request.url);
        dispatcher.dispatch(request, response);
    } catch(err) {
        console.log(err);
    }
}

var server = http.createServer(handleRequest);

server.listen(port, function(){
    console.log("Server listening on: http://0.0.0.0:%s", port);
});
