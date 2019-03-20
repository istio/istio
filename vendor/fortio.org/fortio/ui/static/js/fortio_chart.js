// Copyright 2017 Istio Authors.
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

// TODO: object-ify

var linearXAxe = {
  type: 'linear',
  scaleLabel: {
    display: true,
    labelString: 'Response time in ms',
    ticks: {
      min: 0,
      beginAtZero: true
    }
  }
}
var logXAxe = {
  type: 'logarithmic',
  scaleLabel: {
    display: true,
    labelString: 'Response time in ms (log scale)'
  },
  ticks: {
        // min: dataH[0].x, // newer chart.js are ok with 0 on x axis too
    callback: function (tick, index, ticks) {
      return tick.toLocaleString()
    }
  }
}
var linearYAxe = {
  id: 'H',
  type: 'linear',
  ticks: {
    beginAtZero: true
  },
  scaleLabel: {
    display: true,
    labelString: 'Count'
  }
}
var logYAxe = {
  id: 'H',
  type: 'logarithmic',
  display: true,
  ticks: {
        // min: 1, // log mode works even with 0s
        // Needed to not get scientific notation display:
    callback: function (tick, index, ticks) {
      return tick.toString()
    }
  },
  scaleLabel: {
    display: true,
    labelString: 'Count (log scale)'
  }
}

var chart = {}
var overlayChart = {}
var mchart = {}

function myRound (v, digits = 6) {
  var p = Math.pow(10, digits)
  return Math.round(v * p) / p
}

function pad (n) {
  return (n < 10) ? ('0' + n) : n
}

function formatDate (dStr) {
  var d = new Date(dStr)
  return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + ' ' +
        pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds())
}

function makeTitle (res) {
  var title = []
  if (res.Labels !== '') {
    if (res.URL) { // http results
        title.push(res.Labels + ' - ' + res.URL + ' - ' + formatDate(res.StartTime))
    } else { // grpc results
        title.push(res.Labels + ' - ' + res.Destination + ' - ' + formatDate(res.StartTime))
    }
  }
  var percStr = 'min ' + myRound(1000.0 * res.DurationHistogram.Min, 3) + ' ms, average ' + myRound(1000.0 * res.DurationHistogram.Avg, 3) + ' ms'
  if (res.DurationHistogram.Percentiles) {
    for (var i = 0; i < res.DurationHistogram.Percentiles.length; i++) {
      var p = res.DurationHistogram.Percentiles[i]
      percStr += ', p' + p.Percentile + ' ' + myRound(1000 * p.Value, 2) + ' ms'
    }
  }
  percStr += ', max ' + myRound(1000.0 * res.DurationHistogram.Max, 3) + ' ms'
  var statusOk = res.RetCodes[200]
  if (!statusOk) { // grpc results
    statusOk = res.RetCodes[1]
  }
  var total = res.DurationHistogram.Count
  var errStr = 'no error'
  if (statusOk !== total) {
    if (statusOk) {
      errStr = myRound(100.0 * (total - statusOk) / total, 2) + '% errors'
    } else {
      errStr = '100% errors!'
    }
  }
  title.push('Response time histogram at ' + res.RequestedQPS + ' target qps (' +
        myRound(res.ActualQPS, 1) + ' actual) ' + res.NumThreads + ' connections for ' +
        res.RequestedDuration + ' (actual time ' + myRound(res.ActualDuration / 1e9, 1) + 's), ' +
        errStr)
  title.push(percStr)
  return title
}

function fortioResultToJsChartData (res) {
  var dataP = [{
    x: 0.0,
    y: 0.0
  }]
  var len = res.DurationHistogram.Data.length
  var prevX = 0.0
  var prevY = 0.0
  for (var i = 0; i < len; i++) {
    var it = res.DurationHistogram.Data[i]
    var x = myRound(1000.0 * it.Start)
    if (i === 0) {
      // Extra point, 1/N at min itself
      dataP.push({
        x: x,
        y: myRound(100.0 / res.DurationHistogram.Count, 3)
      })
    } else {
      if (prevX !== x) {
        dataP.push({
          x: x,
          y: prevY
        })
      }
    }
    x = myRound(1000.0 * it.End)
    var y = myRound(it.Percent, 3)
    dataP.push({
      x: x,
      y: y
    })
    prevX = x
    prevY = y
  }
  var dataH = []
  var prev = 1000.0 * res.DurationHistogram.Data[0].Start
  for (i = 0; i < len; i++) {
    it = res.DurationHistogram.Data[i]
    var startX = 1000.0 * it.Start
    var endX = 1000.0 * it.End
    if (startX !== prev) {
      dataH.push({
        x: myRound(prev),
        y: 0
      }, {
        x: myRound(startX),
        y: 0
      })
    }
    dataH.push({
      x: myRound(startX),
      y: it.Count
    }, {
      x: myRound(endX),
      y: it.Count
    })
    prev = endX
  }
  return {
    title: makeTitle(res),
    dataP: dataP,
    dataH: dataH
  }
}

function showChart (data) {
  makeChart(data)
  // Load configuration (min, max, isLogarithmic, ...) from the update form.
  updateChartOptions(chart)
  toggleVisibility()
}

function toggleVisibility () {
  document.getElementById('running').style.display = 'none'
  document.getElementById('cc1').style.display = 'block'
  document.getElementById('update').style.visibility = 'visible'
}

function makeOverlayChartTitle (titleA, titleB) {
  // Each string in the array is a separate line
  return [
    'A: ' + titleA[0], titleA[1], // Skip 3rd line.
    '',
    'B: ' + titleB[0], titleB[1], // Skip 3rd line.
  ]
}

function makeOverlayChart (dataA, dataB) {
  var chartEl = document.getElementById('chart1')
  chartEl.style.visibility = 'visible'
  if (Object.keys(overlayChart).length !== 0) {
    return
  }
  deleteSingleChart()
  deleteMultiChart()
  var ctx = chartEl.getContext('2d')
  var title = makeOverlayChartTitle(dataA.title, dataB.title)
  overlayChart = new Chart(ctx, {
    type: 'line',
    data: {
      // "Cumulative %" datasets are listed first so they are drawn on top of the histograms.
      datasets: [{
        label: 'A: Cumulative %',
        data: dataA.dataP,
        fill: false,
        yAxisID: 'P',
        stepped: true,
        backgroundColor: 'rgba(134, 87, 167, 1)',
        borderColor: 'rgba(134, 87, 167, 1)',
        cubicInterpolationMode: 'monotone'
      }, {
        label: 'B: Cumulative %',
        data: dataB.dataP,
        fill: false,
        yAxisID: 'P',
        stepped: true,
        backgroundColor: 'rgba(204, 102, 0)',
        borderColor: 'rgba(204, 102, 0)',
        cubicInterpolationMode: 'monotone'
      }, {
        label: 'A: Histogram: Count',
        data: dataA.dataH,
        yAxisID: 'H',
        pointStyle: 'rect',
        radius: 1,
        borderColor: 'rgba(87, 167, 134, .9)',
        backgroundColor: 'rgba(87, 167, 134, .75)',
        lineTension: 0
      }, {
        label: 'B: Histogram: Count',
        data: dataB.dataH,
        yAxisID: 'H',
        pointStyle: 'rect',
        radius: 1,
        borderColor: 'rgba(36, 64, 238, .9)',
        backgroundColor: 'rgba(36, 64, 238, .75)',
        lineTension: 0
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      title: {
        display: true,
        fontStyle: 'normal',
        text: title
      },
      scales: {
        xAxes: [
          linearXAxe
        ],
        yAxes: [{
          id: 'P',
          position: 'right',
          ticks: {
            beginAtZero: true,
            max: 100
          },
          scaleLabel: {
            display: true,
            labelString: '%'
          }
        },
          linearYAxe
        ]
      }
    }
  })
  updateChart(overlayChart)
}

function makeChart (data) {
  var chartEl = document.getElementById('chart1')
  chartEl.style.visibility = 'visible'
  if (Object.keys(chart).length === 0) {
    deleteOverlayChart()
    deleteMultiChart()
      // Creation (first or switch) time
    var ctx = chartEl.getContext('2d')
    chart = new Chart(ctx, {
      type: 'line',
      data: {
        datasets: [{
          label: 'Cumulative %',
          data: data.dataP,
          fill: false,
          yAxisID: 'P',
          stepped: true,
          backgroundColor: 'rgba(134, 87, 167, 1)',
          borderColor: 'rgba(134, 87, 167, 1)',
          cubicInterpolationMode: 'monotone'
        },
        {
          label: 'Histogram: Count',
          data: data.dataH,
          yAxisID: 'H',
          pointStyle: 'rect',
          radius: 1,
          borderColor: 'rgba(87, 167, 134, .9)',
          backgroundColor: 'rgba(87, 167, 134, .75)',
          lineTension: 0
        }
        ]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        title: {
          display: true,
          fontStyle: 'normal',
          text: data.title
        },
        scales: {
          xAxes: [
            linearXAxe
          ],
          yAxes: [{
            id: 'P',
            position: 'right',
            ticks: {
              beginAtZero: true,
              max: 100
            },
            scaleLabel: {
              display: true,
              labelString: '%'
            }
          },
            linearYAxe
          ]
        }
      }
    })
      // TODO may need updateChart() if we persist settings even the first time
  } else {
    chart.data.datasets[0].data = data.dataP
    chart.data.datasets[1].data = data.dataH
    chart.options.title.text = data.title
    updateChart(chart)
  }
}

function getUpdateForm () {
  var form = document.getElementById('updtForm')
  var xMin = form.xmin.value.trim()
  var xMax = form.xmax.value.trim()
  var xIsLogarithmic = form.xlog.checked
  var yIsLogarithmic = form.ylog.checked
  return { xMin, xMax, xIsLogarithmic, yIsLogarithmic }
}

function getSelectedResults () {
  // Undefined if on "graph-only" page
  var select = document.getElementById('files')
  var selectedResults
  if (select) {
    var selectedOptions = select.selectedOptions
    selectedResults = []
    for (var option of selectedOptions) {
      selectedResults.push(option.text)
    }
  } else {
    selectedResults = undefined
  }
  return selectedResults
}

function updateQueryString () {
  var location = document.location
  var params = new URLSearchParams(location.search)
  var form = getUpdateForm()
  params.set('xMin', form.xMin)
  params.set('xMax', form.xMax)
  params.set('xLog', form.xIsLogarithmic)
  params.set('yLog', form.yIsLogarithmic)
  var selectedResults = getSelectedResults()
  params.delete('sel')
  if (selectedResults) {
    for (var result of selectedResults) {
      params.append('sel', result)
    }
  }
  window.history.replaceState({}, '', `${location.pathname}?${params}`)
}

function updateChartOptions (chart) {
  var form = getUpdateForm()
  var scales = chart.config.options.scales
  var newXMin = parseFloat(form.xMin)
  var newXAxis = form.xIsLogarithmic ? logXAxe : linearXAxe
  var newYAxis = form.yIsLogarithmic ? logYAxe : linearYAxe
  chart.config.options.scales = {
    xAxes: [newXAxis],
    yAxes: [scales.yAxes[0], newYAxis]
  }
  chart.update() // needed for scales.xAxes[0] to exist
  var newNewXAxis = chart.config.options.scales.xAxes[0]
  newNewXAxis.ticks.min = form.xMin === '' ? undefined : newXMin
  var formXMax = form.xMax
  newNewXAxis.ticks.max = formXMax === '' || formXMax === 'max' ?
      undefined :
      parseFloat(formXMax)
  chart.update()
}

function objHasProps (obj) {
  return Object.keys(obj).length > 0
}

function getCurrentChart () {
  var currentChart
  if (objHasProps(chart)) {
    currentChart = chart
  } else if (objHasProps(overlayChart)) {
    currentChart = overlayChart
  } else if (objHasProps(mchart)) {
    currentChart = mchart
  } else {
    currentChart = undefined
  }
  return currentChart
}

var timeoutID = 0
function updateChart (chart = getCurrentChart()) {
  updateChartOptions(chart)
  if (timeoutID > 0) {
    clearTimeout(timeoutID)
  }
  timeoutID = setTimeout("updateQueryString()", 750)
}

function multiLabel (res) {
  var l = formatDate(res.StartTime)
  if (res.Labels !== '') {
    l += ' - ' + res.Labels
  }
  return l
}

function findData (slot, idx, res, p) {
  // Not very efficient but there are only a handful of percentiles
  var pA = res.DurationHistogram.Percentiles
  if (!pA) {
//    console.log('No percentiles in res', res)
    return
  }
  var pN = Number(p)
  for (var i = 0; i < pA.length; i++) {
    if (pA[i].Percentile === pN) {
      mchart.data.datasets[slot].data[idx] = 1000.0 * pA[i].Value
      return
    }
  }
  console.log('Not Found', p, pN, pA)
  // not found, not set
}

function fortioAddToMultiResult (i, res) {
  mchart.data.labels[i] = multiLabel(res)
  mchart.data.datasets[0].data[i] = 1000.0 * res.DurationHistogram.Min
  findData(1, i, res, '50')
  mchart.data.datasets[2].data[i] = 1000.0 * res.DurationHistogram.Avg
  findData(3, i, res, '75')
  findData(4, i, res, '90')
  findData(5, i, res, '99')
  findData(6, i, res, '99.9')
  mchart.data.datasets[7].data[i] = 1000.0 * res.DurationHistogram.Max
  mchart.data.datasets[8].data[i] = res.ActualQPS
}

function endMultiChart (len) {
  mchart.data.labels = mchart.data.labels.slice(0, len)
  for (var i = 0; i < mchart.data.datasets.length; i++) {
    mchart.data.datasets[i].data = mchart.data.datasets[i].data.slice(0, len)
  }
  mchart.update()
}

function deleteOverlayChart () {
  if (Object.keys(overlayChart).length === 0) {
    return
  }
  overlayChart.destroy()
  overlayChart = {}
}

function deleteMultiChart () {
  if (Object.keys(mchart).length === 0) {
    return
  }
  mchart.destroy()
  mchart = {}
}

function deleteSingleChart () {
  if (Object.keys(chart).length === 0) {
    return
  }
  chart.destroy()
  chart = {}
}

function makeMultiChart () {
  document.getElementById('running').style.display = 'none'
  document.getElementById('update').style.visibility = 'hidden'
  var chartEl = document.getElementById('chart1')
  chartEl.style.visibility = 'visible'
  if (Object.keys(mchart).length !== 0) {
    return
  }
  deleteSingleChart()
  deleteOverlayChart()
  var ctx = chartEl.getContext('2d')
  mchart = new Chart(ctx, {
    type: 'line',
    data: {
      labels: [],
      datasets: [
        {
          label: 'Min',
          fill: false,
          stepped: true,
          borderColor: 'hsla(111, 100%, 40%, .8)',
          backgroundColor: 'hsla(111, 100%, 40%, .8)'
        },
        {
          label: 'Median',
          fill: false,
          stepped: true,
          borderDash: [5, 5],
          borderColor: 'hsla(220, 100%, 40%, .8)',
          backgroundColor: 'hsla(220, 100%, 40%, .8)'
        },
        {
          label: 'Avg',
          fill: false,
          stepped: true,
          backgroundColor: 'hsla(266, 100%, 40%, .8)',
          borderColor: 'hsla(266, 100%, 40%, .8)'
        },
        {
          label: 'p75',
          fill: false,
          stepped: true,
          backgroundColor: 'hsla(60, 100%, 40%, .8)',
          borderColor: 'hsla(60, 100%, 40%, .8)'
        },
        {
          label: 'p90',
          fill: false,
          stepped: true,
          backgroundColor: 'hsla(45, 100%, 40%, .8)',
          borderColor: 'hsla(45, 100%, 40%, .8)'
        },
        {
          label: 'p99',
          fill: false,
          stepped: true,
          backgroundColor: 'hsla(30, 100%, 40%, .8)',
          borderColor: 'hsla(30, 100%, 40%, .8)'
        },
        {
          label: 'p99.9',
          fill: false,
          stepped: true,
          backgroundColor: 'hsla(15, 100%, 40%, .8)',
          borderColor: 'hsla(15, 100%, 40%, .8)'
        },
        {
          label: 'Max',
          fill: false,
          stepped: true,
          borderColor: 'hsla(0, 100%, 40%, .8)',
          backgroundColor: 'hsla(0, 100%, 40%, .8)'
        },
        {
          label: 'QPS',
          yAxisID: 'qps',
          fill: false,
          stepped: true,
          borderColor: 'rgba(0, 0, 0, .8)',
          backgroundColor: 'rgba(0, 0, 0, .8)'
        }
      ]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      title: {
        display: true,
        fontStyle: 'normal',
        text: ['Latency in milliseconds']
      },
      elements: {
        line: {
          tension: 0 // disables bezier curves
        }
      },
      scales: {
        yAxes: [{
          id: 'ms',
          ticks: {
            beginAtZero: true
          },
          scaleLabel: {
            display: true,
            labelString: 'ms'
          }
        }, {
          id: 'qps',
          position: 'right',
          ticks: {
            beginAtZero: true
          },
          scaleLabel: {
            display: true,
            labelString: 'QPS'
          }
        }]
      }
    }
  })
  // Hide QPS axis on clicking QPS dataset.
  mchart.options.legend.onClick = (event, legendItem) => {
    // Toggle dataset hidden (default behavior).
    var dataset = mchart.data.datasets[legendItem.datasetIndex]
    dataset.hidden = !dataset.hidden
    if (dataset.label === 'QPS') {
      // Toggle QPS y-axis.
      var qpsYAxis = mchart.options.scales.yAxes[1]
      qpsYAxis.display = !qpsYAxis.display
    }
    mchart.update()
  }
}

function runTestForDuration (durationInSeconds) {
  var progressBar = document.getElementById('progressBar')
  if (durationInSeconds <= 0) {
      // infinite case
    progressBar.removeAttribute('value')
    return
  }
  var startTimeMillis = Date.now()
  var updatePercentage = function () {
    var barPercentage = Math.min(100, (Date.now() - startTimeMillis) / (10 * durationInSeconds))
    progressBar.value = barPercentage
    if (barPercentage < 100) {
      setTimeout(updatePercentage, 50 /* milliseconds */) // 20fps
    }
  }
  updatePercentage()
}

var lastDuration = ''

function toggleDuration (el) {
  var d = document.getElementById('duration')
  if (el.checked) {
    lastDuration = d.value
    d.value = ''
  } else {
    d.value = lastDuration
  }
}
