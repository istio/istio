from __future__ import print_function
import json
import os
import sys

target_dir="."

if len(sys.argv) > 1:
    target_dir = sys.argv[1]

# Converts Fortio result output data into a CSV line.
def csv_line(data):
    rawLabels = data['Labels'].split()

    labels = ",".join([l for l in rawLabels if l[0] != 'Q' and l[0] != 'T' and l[0] != 'C'])

    qps = data['RequestedQPS']
    duration = data['RequestedDuration']
    clients = data['NumThreads']
    min = data['DurationHistogram']['Min']
    max = data['DurationHistogram']['Max']
    avg = data['DurationHistogram']['Avg']

    p50 = [e['Value'] for e in data['DurationHistogram']['Percentiles'] if e['Percentile'] == 50][0]
    p75 = [e['Value'] for e in data['DurationHistogram']['Percentiles'] if e['Percentile'] == 75][0]
    p90 = [e['Value'] for e in data['DurationHistogram']['Percentiles'] if e['Percentile'] == 90][0]
    p99 = [e['Value'] for e in data['DurationHistogram']['Percentiles'] if e['Percentile'] == 99][0]
    p99d9 = [e['Value'] for e in data['DurationHistogram']['Percentiles'] if e['Percentile'] == 99.9][0]

    return ("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s" % (labels, qps, duration, clients, min, max, avg, p50, p75, p90, p99, p99d9))

# Print the header line
print("Label,Driver,Target,qps,duration,clients,min,max,avg,p50,p75,p90,p99,p99.9")

# For each json file in current dir, interpret it as Fortio result json file and print a csv line for it.
for fn in os.listdir(target_dir):
    fullfn = os.path.join(target_dir, fn)
    if os.path.isfile(fullfn) and fullfn.endswith('.json'):
        with open(fullfn) as f:
            data = json.load(f)
            print(csv_line(data))

