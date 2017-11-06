"use strict";

var express = require('express');
var fs = require('fs');
var path = require('path');
var mime = require('mime');

function service() {
  var send_file = function(file, res) {
    res.setHeader('Content-type', mime.lookup(file));
    var filestream = fs.createReadStream(file);
    filestream.pipe(res);
  };

  var server = express();

  // Tracing middleware.
  server.use(function(req, res, next) {
    console.log(req.method, req.originalUrl);
    console.log(req.headers);
    console.log();
    next();
  })

  server.get('/cert', function(req, res) {
    send_file(__dirname + '/node_agent.crt', res);
  });

  server.get('/key', function(req, res) {
    send_file(__dirname + '/node_agent.key', res);
  });

  server.get('/root', function(req, res) {
    send_file(__dirname + '/istio_ca.crt', res);
  });

  return server;
}

if (module.parent) {
  module.exports = service;
} else {
  var server = service().listen(process.env.PORT || '8080', '0.0.0.0', function() {
    var host = server.address().address;
    var port = server.address().port;

    console.log('App listening at http://%s:%s', host, port);
  })
}