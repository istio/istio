package metrics

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
)

var (
	TCPMetricsError = monitoring.NewSum(
		"tcp_metrics_error",
		"The total error times while generate tcp statistics.",
	)
	TCPSent = monitoring.NewSum(
		"tcp_sent",
		"The total number of tcp packets send out.",
	)
	TCPRetransmit = monitoring.NewSum(
		"tcp_retransmit",
		"The total number of tcp packets retransmitted.",
	)
	TCPLostRetransmit = monitoring.NewSum(
		"tcp_lost_retransmit",
		"The total number of tcp packets retransmitted more than one time.",
	)
	TCPSYNDrop = monitoring.NewSum(
		"tcp_syn_drop",
		"The total number of tcp SYNs to LISTEN sockets dropped.",
	)
	TCPSYNRetransmit = monitoring.NewSum(
		"tcp_syn_retransmit",
		"The total number of tcp SYN retransmitted.",
	)
	TCPResetUnexpectedData = monitoring.NewSum(
		"tcp_reset_unexpected_data",
		"The total number of tcp_reset_unexpected_data.",
	)
	TCPResetEarlyUserClose = monitoring.NewSum(
		"tcp_reset_early_user_close",
		"The total number of tcp_reset_early_user_close.",
	)
	TCPResetTimeout = monitoring.NewSum(
		"tcp_reset_timeout",
		"The total number of tcp_reset_timeout.",
	)
)

var lastValue = map[string]int{
	"tcp_sent":                   0,
	"tcp_retransmit":             0,
	"tcp_lost_retransmit":        0,
	"tcp_syn_drop":               0,
	"tcp_syn_retransmit":         0,
	"tcp_reset_unexpected_data":  0,
	"tcp_reset_early_user_close": 0,
	"tcp_reset_timeout":          0,
}

func parseValue(s, t string, pos int) (int, error) {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, t) {
			lt := strings.TrimSpace(l)
			parts := strings.Split(lt, " ")
			if pos == -1 {
				pos = len(parts) - 1
			} else {
				pos = 0
			}
			v, err := strconv.Atoi(parts[pos])
			if err != nil {
				return 0, fmt.Errorf("cannot convert!")
			} else {
				return v, nil
			}
		}
	}
	return 0, nil
}

func updateTcpStatistic() {
	cmd := exec.Command("netstat", "-s")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		fmt.Println("Error while run command: ", err)
		return
	}

	if tcpSent, err := parseValue(out.String(), "segments send out", 0); err != nil {
		log.Errorf("Metric tcp_sent parse error: %v", err)
		TCPMetricsError.Increment()
	} else {
		TCPSent.RecordInt(int64(tcpSent - lastValue["tcp_sent"]))
		lastValue["tcp_sent"] = tcpSent
	}

	if tcpRetransmit, err := parseValue(out.String(), "segments retransmited", 0); err != nil {
		log.Errorf("Metric tcp_retransmit parse error: %v", err)
		TCPMetricsError.Increment()
	} else {
		TCPRetransmit.RecordInt(int64(tcpRetransmit - lastValue["tcp_retransmit"]))
		lastValue["tcp_retransmit"] = tcpRetransmit
	}

	if tcpLostRetransmit, err := parseValue(out.String(), "TCPLostRetransmit", -1); err != nil {
		log.Errorf("Metric tcp_lost_retransmit parse error: %v", err)
		TCPMetricsError.Increment()
	} else {
		TCPLostRetransmit.RecordInt(int64(tcpLostRetransmit - lastValue["tcp_lost_retransmit"]))
		lastValue["tcp_lost_retransmit"] = tcpLostRetransmit
	}

	if tcpSynDrop, err := parseValue(out.String(), "SYNs to LISTEN sockets dropped", 0); err != nil {
		log.Errorf("Metric tcp_syn_drop parse error: %v", err)
		TCPMetricsError.Increment()
	} else {
		TCPSYNDrop.RecordInt(int64(tcpSynDrop - lastValue["tcp_syn_drop"]))
		lastValue["tcp_syn_drop"] = tcpSynDrop
	}
	if tcpSynRetransmit, err := parseValue(out.String(), "TCPSynRetrans", -1); err != nil {
		log.Errorf("Metric tcp_syn_retransmit parse error: %v", err)
		TCPMetricsError.Increment()
	} else {
		TCPSYNRetransmit.RecordInt(int64(tcpSynRetransmit - lastValue["tcp_syn_retransmit"]))
		lastValue["tcp_syn_retransmit"] = tcpSynRetransmit
	}

	if tcpResetUnexpectedData, err := parseValue(out.String(), "reset due to unexpected data", 0); err != nil {
		log.Errorf("Metric tcp_reset_unexpected_data parse error: %v", err)
		TCPMetricsError.Increment()
	} else {
		TCPResetUnexpectedData.RecordInt(int64(tcpResetUnexpectedData - lastValue["tcp_reset_unexpected_data"]))
		lastValue["tcp_reset_unexpected_data"] = tcpResetUnexpectedData
	}

	if tcpResetEarlyUserClose, err := parseValue(out.String(), "reset due to early user close", 0); err != nil {
		log.Errorf("Metric tcp_reset_early_user_close parse error: %v", err)
		TCPMetricsError.Increment()
	} else {
		TCPResetEarlyUserClose.RecordInt(int64(tcpResetEarlyUserClose - lastValue["tcp_reset_early_user_close"]))
		lastValue["tcp_reset_early_user_close"] = tcpResetEarlyUserClose
	}

	if tcpResetTimeout, err := parseValue(out.String(), "aborted due to timeout", 0); err != nil {
		log.Errorf("Metric tcp_reset_timeout parse error: %v", err)
		TCPMetricsError.Increment()
	} else {
		TCPResetTimeout.RecordInt(int64(tcpResetTimeout - lastValue["tcp_reset_timeout"]))
		lastValue["tcp_reset_timeout"] = tcpResetTimeout
	}
}

func init() {
	monitoring.MustRegister(
		TCPMetricsError,
		TCPSent,
		TCPRetransmit,
		TCPLostRetransmit,
		TCPSYNDrop,
		TCPSYNRetransmit,
		TCPResetUnexpectedData,
		TCPResetEarlyUserClose,
		TCPResetTimeout,
	)
	go func() {
		for {
			time.Sleep(2 * time.Second)
			updateTcpStatistic()
		}
	}()
}
