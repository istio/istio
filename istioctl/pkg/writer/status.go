package writer

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

var statusTimeFormat = "2006-01-02 15:04:05.999999999 UTC"

// StatusWriter enables printing of sync status using multiple []byte Pilot responses
type StatusWriter struct {
	Writer io.Writer
}

// PrintAll takes a slice of byte slices and outputs them using a tabwriter
func (s *StatusWriter) PrintAll(statuses [][]byte) error {
	w, fullStatus, err := s.setupStatusPrint(statuses)
	if err != nil {
		return err
	}
	for _, status := range fullStatus {
		if err := statusPrintln(w, status); err != nil {
			return err
		}
	}
	return w.Flush()
}

// PrintSingle takes a slice of byte slices and outputs them using a tabwriter
func (s *StatusWriter) PrintSingle(statuses [][]byte, podName string) error {
	w, fullStatus, err := s.setupStatusPrint(statuses)
	if err != nil {
		return err
	}
	for _, status := range fullStatus {
		if strings.Contains(status.ProxyID, podName) {
			if err := statusPrintln(w, status); err != nil {
				return err
			}
		}
	}
	return w.Flush()
}

func (s *StatusWriter) setupStatusPrint(statuses [][]byte) (*tabwriter.Writer, []v2.SyncStatus, error) {
	w := new(tabwriter.Writer)
	w.Init(s.Writer, 0, 8, 5, '\t', 0)
	fmt.Fprintln(w, "PROXY\tSTATUS\tSENT\tACKNOWLEDGED")
	fullStatus := []v2.SyncStatus{}
	for _, status := range statuses {
		ss := []v2.SyncStatus{}
		err := json.Unmarshal(status, &ss)
		if err != nil {
			return nil, nil, err
		}
		fullStatus = append(fullStatus, ss...)
	}
	sort.Slice(fullStatus, func(i, j int) bool {
		return fullStatus[i].ProxyID < fullStatus[j].ProxyID
	})
	return w, fullStatus, nil
}

func parseMonotonicTime(t string) (time.Time, error) {
	split := strings.Split(t, " m=") // strip monotonic clock suffix
	return time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", split[0])
}

func statusPrintln(w *tabwriter.Writer, status v2.SyncStatus) error {
	sent, err := parseMonotonicTime(status.Sent)
	if err != nil {
		return err
	}
	acked, err := parseMonotonicTime(status.Acked)
	if err != nil {
		return err
	}
	synced := "DIVERGED"
	if sent == acked {
		synced = "SYNCED"
	}
	fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", status.ProxyID, synced, sent.Format(statusTimeFormat), acked.Format(statusTimeFormat))
	return nil
}
