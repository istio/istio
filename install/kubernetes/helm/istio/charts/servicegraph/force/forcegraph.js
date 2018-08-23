// The MIT License (MIT)

// Copyright (c) 2013 Tim Dwyer
// Copyright (c) 2017 Istio Authors

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

var graphDiv = d3.select("#graph")
    .on("click", function(d){select(null);});

var width = graphDiv.node().getBoundingClientRect().width - 20;
var height = 1000;
var infoHeight = 300;

var graphDivHeight = d3.select("#total").node().getBoundingClientRect().height - 20;

hideInfo();

function showInfo() {
    var shortHeight = graphDivHeight - 300
    graphDiv.style("height", shortHeight + "px");
    d3.select("#info").style("display", "block");
}
function hideInfo() {
    graphDiv.style("height", graphDivHeight + "px");
    d3.select("#info").style("display", "none");
}

var d3cola = cola.d3adaptor(d3)
    .avoidOverlaps(true)
    .convergenceThreshold(1e-3)
    .flowLayout('y', 150)
    .jaccardLinkLengths(150)
    .size([width, height]);

var outer = graphDiv.append("svg")
    .attr('width',width)
    .attr('height',height);

var vis = outer
    .append('g');

var lineFunction = d3.line()
    .x(function (d) { return d.x; })
    .y(function (d) { return d.y; });

var link, node, label;
var margin = 8, pad = 6;

outer.append('svg:defs').append('svg:marker')
    .attr('id','end-arrow')
    .attr('viewBox','0 -5 10 10')
    .attr('refX',8)
    .attr('markerWidth',6)
    .attr('markerHeight',6)
    .attr('orient','auto')
    .append('svg:path')
    .attr('d','M0,-5L10,0L0,5L2,0')
    .attr('stroke-width','0px')
    .attr('fill','#000');

d3cola.on("tick", function () {
    node.each(function (d) { d.innerBounds = d.bounds.inflate(-margin); })
        .attr("x", function (d) { return d.innerBounds.x; })
        .attr("y", function (d) { return d.innerBounds.y; })
        .attr("width", function (d) {
            return d.innerBounds.width();
        })
        .attr("height", function (d) {
            return d.innerBounds.height();
        });

    link.attr("d", function (d) {
        var route = cola.makeEdgeBetween(d.source.innerBounds, d.target.innerBounds, 5);
        return lineFunction([route.sourceIntersection, route.arrowStart]);
    });

    label
        .attr("x", function (d) { return d.x })
        .attr("y", function (d) { return d.y + (margin + pad) / 2 });

});

var endpoint = "/d3graph";

(function(){
    var query={};
    var params = [];
    var i=0;
    var tele;
    var search_values=location.search.replace('\?','').split('&');
    for(i=0;i<search_values.length;i++){
        telem=search_values[i].split('=');
        query[telem[0]]=telem[1];
    }
    for (var k in query) {
        if (query.hasOwnProperty(k)) {
            params.push(k + "=" + query[k]);
        }
    }
    var queryParams = params.join("&");
    if (queryParams !== "") {
        endpoint = endpoint + "?" + queryParams;
    }
}());

refresh()
d3.interval(refresh, 5000)

function refresh() {
    d3.json(endpoint, updateData);
    return true;
}

var graphData;

function linkName(d) {
    var source, dest;
    if (Number.isInteger(d.source)) {
        source = graphData.nodes[d.source].name;
    } else {
        source = d.source.name;
    }
    if (Number.isInteger(d.source)) {
        dest = graphData.nodes[d.target].name;
    } else {
        dest = d.target.name;
    }

    return source + "_" + dest;
}

function updateData(data) {
    if (graphData != undefined) {
        data.nodes.forEach(function(newNode) {
            var found = graphData.nodes.find(function(n){return n.name == newNode.name});
            if (found != undefined) {
                newNode.x = found.x;
                newNode.y = found.y;
            }
        });
    }
    graphData = data;

    d3cola.stop();
    delete d3cola._lastStress;
    delete d3cola._alpha;
    delete d3cola._descent;
    delete d3cola._rootGroup;

    d3cola
        .nodes(data.nodes)
        .links(data.links);

    link = vis.selectAll(".link")
        .data(data.links, linkName);
    link
        .enter().append("path")
        .attr("class", "link");
    link
        .exit().remove();
    link = vis.selectAll(".link")
        .data(data.links, linkName);

    node = vis.selectAll(".node")
        .data(data.nodes, function(d){return d.name;});
    node
        .enter().append("rect")
        .classed("node", true)
        .attr('rx',5)
        .attr('ry',5)
        .call(d3cola.drag)
        .on("click", function(d){select(d);}, true)
        .on("mouseenter", function(d){highlight(d);})
        .on("mouseleave", function(d){highlight(null);});
    node
        .exit().remove();
    node = vis.selectAll(".node")
        .data(data.nodes, function(d){return d.name;});

    label = vis.selectAll(".label")
        .data(data.nodes, function(d){return d.name;});
    label
        .enter().append("text")
        .attr("class", "label")
        .text(function (d) { return d.name; })
        .call(d3cola.drag)
        .on("click", function(d){select(d);}, true)
        .on("mouseenter", function(d){highlight(d);})
        .on("mouseleave", function(d){highlight(null);});
    label
        .exit().remove();
    label = vis.selectAll(".label")
        .data(data.nodes, function(d){return d.name;});
    label
        .each(function (d) {
            var b = this.getBBox();
            var extra = 2 * margin + 2 * pad;
            d.width = b.width + extra;
            d.height = b.height + extra;
        });

    d3cola
        .start();

    updateInfo();
}

var source   = document.getElementById("info-template").innerHTML;
var template = Handlebars.compile(source);

function updateInfo() {
    if (selected != null) {
        var nodeData = {
            name:selected.name,
            incoming:[],
            outgoing:[]
        };
        graphData.links.forEach(function(l) {
            if (l.source.name == selected.name) {
                nodeData.outgoing.push({destination:l.target.name, ops:l.labels["reqs/sec"]});
            }
            if (l.target.name == selected.name) {
                nodeData.incoming.push({source:l.source.name, ops:l.labels["reqs/sec"]});
            }
        });
        nodeData.incoming.sort(function(a, b){return a.source > b.source;});
        nodeData.outgoing.sort(function(a, b){return a.destination > b.destination;});
        var html = template(nodeData);
        d3.select("#info").html(html);
    }
}

var selected = null;

function highlight(obj) {
    if (!selected) {
        if (obj) {
            node.classed("darken", function(d){return obj !== d;});
            label.classed("darken", function(d){return obj !== d;});
            link.classed("darken", function(d){return obj !== d.source && obj !== d.target});
        } else {
            node.classed("darken", false);
            label.classed("darken", false);
            link.classed("darken", false);
        }
    }
}

function select(obj) {
    d3.event.stopPropagation();
    if (obj && selected !== obj) {
        node.classed("darken", function(d){return obj !== d;});
        label.classed("darken", function(d){return obj !== d;});
        link.classed("darken", function(d){return obj !== d.source && obj !== d.target});
        selected = obj;
        updateInfo();
        showInfo();
    } else {
        node.classed("darken", false);
        label.classed("darken", false);
        link.classed("darken", false);
        selected = null;
        hideInfo();
    }
}
