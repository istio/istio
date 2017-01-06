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
// An example implementation of Echo backend.

"use strict";

var http = require('http');
var server = http.createServer();

var totalReceived = 0;
var totalData = 0;

server.on('request', function(req, res) {
  totalReceived += 1;
  var method = req.method;
  var url = req.url;
  if (method == 'GET' && url == '/version') {
      res.writeHead(200, {"Content-Type": "application/json"});
      res.write('{"version":"${VERSION}"}');
      res.end();
      return;
  }
  req.on('data', function(chunk) {
    totalData += chunk.length;
    res.write(chunk);
  })
  req.on('end', function() {
    res.end();
  });

  var cl = req.headers['content-length'];
  var ct = req.headers['content-type'];

  var headers = {};
  if (cl !== undefined) {
    headers['Content-Length'] = cl;
  }
  if (ct !== undefined) {
    headers['Content-Type'] = ct;
  }

  res.writeHead(200, headers);
  req.resume();
});

var totalConnection = 0;

server.on('connection', function(socket) {
  totalConnection += 1;
});

setInterval(function() {
  console.log("Requests received:", totalReceived, " Data: ", totalData, " Connection: ", totalConnection);
}, 1000);

var port = process.env.PORT || 8080;

server.listen(port, function() {
  console.log('Listening on port', port);
});
