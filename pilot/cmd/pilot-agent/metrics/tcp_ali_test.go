package metrics

import (
	"testing"
)

func TestParseValue(t *testing.T) {
	testCases := []struct {
		name, input, target string
		pos, expected       int
	}{
		{
			name: "TCPSentCase1",
			input: `Tcp:
			220 active connections openings
			162 passive connection openings
			11 failed connection attempts
			1 connection resets received
			14 connections established
			49040 segments received
			34255 segments send out
			1 segments retransmited
			0 bad segments received.
			115 resets sent`,
			target:   "segments send out",
			pos:      0,
			expected: 34255,
		},
		{
			name: "tcpRetransmitCase1",
			input: `Tcp:
			220 active connections openings
			162 passive connection openings
			11 failed connection attempts
			1 connection resets received
			14 connections established
			49040 segments received
			34255 segments send out
			1 segments retransmited
			0 bad segments received.
			115 resets sent`,
			target:   "segments retransmited",
			pos:      0,
			expected: 1,
		},
		{
			name: "tcpLostRetransmitCase1",
			input: `TcpExt:
			1119 TCP sockets finished time wait in slow timer
			290 delayed acks sent
			Quick ack mode was activated 8 times
			46007 packet headers predicted
			4032 acknowledgments not containing data payload received
			2212 predicted acknowledgments
			TCPLostRetransmit: 4`,
			target:   "TCPLostRetransmit",
			pos:      -1,
			expected: 4,
		},
		{
			name: "tcpSynDropCase1",
			input: `TcpExt:    
			74132 times the listen queue of a socket overflowed
			74133 SYNs to LISTEN sockets dropped
			107557040 packet headers predicted
			86035790 acknowledgments not containing data payload received
			TCPDSACKRecv: 329855
			TCPDSACKOfoRecv: 89
			58697 connections reset due to unexpected data
			226672 connections reset due to early user close
			1070 connections aborted due to timeout`,
			target:   "SYNs to LISTEN sockets dropped",
			pos:      0,
			expected: 74133,
		},
		{
			name: "tcpSynRetransmitCase1",
			input: `TcpExt:    
			74132 times the listen queue of a socket overflowed
			74133 SYNs to LISTEN sockets dropped
			107557040 packet headers predicted
			86035790 acknowledgments not containing data payload received
			TCPDSACKRecv: 329855
			TCPDSACKOfoRecv: 89
			58697 connections reset due to unexpected data
			226672 connections reset due to early user close
			1070 connections aborted due to timeout
			TCPSynRetrans: 3`,
			target:   "TCPSynRetrans",
			pos:      -1,
			expected: 3,
		},
		{
			name: "tcpResetUnexpectedDataCase1",
			input: `TcpExt:    
			74132 times the listen queue of a socket overflowed
			74133 SYNs to LISTEN sockets dropped
			107557040 packet headers predicted
			86035790 acknowledgments not containing data payload received
			TCPDSACKRecv: 329855
			TCPDSACKOfoRecv: 89
			58697 connections reset due to unexpected data
			226672 connections reset due to early user close
			1070 connections aborted due to timeout
			TCPSynRetrans: 3`,
			target:   "reset due to unexpected data",
			pos:      0,
			expected: 58697,
		},
		{
			name: "tcpResetEarlyUserCloseCase1",
			input: `TcpExt:    
			74132 times the listen queue of a socket overflowed
			74133 SYNs to LISTEN sockets dropped
			107557040 packet headers predicted
			86035790 acknowledgments not containing data payload received
			TCPDSACKRecv: 329855
			TCPDSACKOfoRecv: 89
			58697 connections reset due to unexpected data
			226672 connections reset due to early user close
			1070 connections aborted due to timeout
			TCPSynRetrans: 3`,
			target:   "reset due to early user close",
			pos:      0,
			expected: 226672,
		},
		{
			name: "tcpResetTimeoutCase1",
			input: `TcpExt:    
			74132 times the listen queue of a socket overflowed
			74133 SYNs to LISTEN sockets dropped
			107557040 packet headers predicted
			86035790 acknowledgments not containing data payload received
			TCPDSACKRecv: 329855
			TCPDSACKOfoRecv: 89
			58697 connections reset due to unexpected data
			226672 connections reset due to early user close
			1070 connections aborted due to timeout
			TCPSynRetrans: 3`,
			target:   "aborted due to timeout",
			pos:      0,
			expected: 1070,
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			if v, _ := parseValue(c.input, c.target, c.pos); v != c.expected {
				t.Errorf("\ninput: %s \ntarget: %s \nexpected: %d \noutput: %d",
					c.input, c.target, c.expected, v)
			}
		})
	}
}
