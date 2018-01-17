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
    title.push(res.Labels + ' - ' + res.URL + ' - ' + formatDate(res.StartTime))
  }
  var percStr = 'min ' + myRound(1000.0 * res.DurationHistogram.Min, 3) + ' ms, average ' + myRound(1000.0 * res.DurationHistogram.Avg, 3) + ' ms'
  if (res.DurationHistogram.Percentiles) {
    for (var i = 0; i < res.DurationHistogram.Percentiles.length; i++) {
      var p = res.DurationHistogram.Percentiles[i]
      percStr += ', p' + p.Percentile + ' ' + myRound(1000 * p.Value, 2) + ' ms'
    }
  }
  percStr += ', max ' + myRound(1000.0 * res.DurationHistogram.Max, 3) + ' ms'
  var httpOk = res.RetCodes[200]
  var total = res.DurationHistogram.Count
  var errStr = 'no error'
  if (httpOk !== total) {
    if (httpOk) {
      errStr = myRound(100.0 * (total - httpOk) / total, 2) + '% errors'
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
  toggleVisibility()
  makeChart(data)
}

function toggleVisibility () {
  document.getElementById('running').style.display = 'none'
  document.getElementById('cc1').style.display = 'block'
  document.getElementById('update').style.visibility = 'visible'
}

function makeChart (data) {
  var chartEl = document.getElementById('chart1')
  chartEl.style.visibility = 'visible'
  if (Object.keys(chart).length === 0) {
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
    updateChart()
  }
}

function setChartOptions () {
  var form = document.getElementById('updtForm')
  var formMin = form.xmin.value.trim()
  var formMax = form.xmax.value.trim()
  var scales = chart.config.options.scales
  var newXAxis
  var newXMin = parseFloat(formMin)
  if (form.xlog.checked) {
    newXAxis = logXAxe
  } else {
    newXAxis = linearXAxe
  }
  if (form.ylog.checked) {
    chart.config.options.scales = {
      xAxes: [newXAxis],
      yAxes: [scales.yAxes[0], logYAxe]
    }
  } else {
    chart.config.options.scales = {
      xAxes: [newXAxis],
      yAxes: [scales.yAxes[0], linearYAxe]
    }
  }
  chart.update() // needed for scales.xAxes[0] to exist
  var newNewXAxis = chart.config.options.scales.xAxes[0]
  if (formMin !== '') {
    newNewXAxis.ticks.min = newXMin
  } else {
    delete newNewXAxis.ticks.min
  }
  if (formMax !== '' && formMax !== 'max') {
    newNewXAxis.ticks.max = parseFloat(formMax)
  } else {
    delete newNewXAxis.ticks.max
  }
}

function updateChart () {
  setChartOptions()
  chart.update()
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
  findData(4, i, res, '99')
  findData(5, i, res, '99.9')
  mchart.data.datasets[6].data[i] = 1000.0 * res.DurationHistogram.Max
}

function endMultiChart (len) {
  mchart.data.labels = mchart.data.labels.slice(0, len)
  for (var i = 0; i < mchart.data.datasets.length; i++) {
    mchart.data.datasets[i].data = mchart.data.datasets[i].data.slice(0, len)
  }
  mchart.update()
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

function makeMultiChart (data) {
  document.getElementById('running').style.display = 'none'
  document.getElementById('update').style.visibility = 'hidden'
  var chartEl = document.getElementById('chart1')
  chartEl.style.visibility = 'visible'
  if (Object.keys(mchart).length !== 0) {
    return
  }
  deleteSingleChart()
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
          backgroundColor: 'hsla(55, 100%, 40%, .8)',
          borderColor: 'hsla(55, 100%, 40%, .8)'
        },
        {
          label: 'p99',
          fill: false,
          stepped: true,
          backgroundColor: 'hsla(40, 100%, 40%, .8)',
          borderColor: 'hsla(40, 100%, 40%, .8)'
        },
        {
          label: 'p99.9',
          fill: false,
          stepped: true,
          backgroundColor: 'hsla(20, 100%, 40%, .8)',
          borderColor: 'hsla(20, 100%, 40%, .8)'
        },
        {
          label: 'Max',
          fill: false,
          stepped: true,
          borderColor: 'hsla(0, 100%, 40%, .8)',
          backgroundColor: 'hsla(0, 100%, 40%, .8)'
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
        }
        ]
      }
    }
  })
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
