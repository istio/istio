package a

func testFunction() {
	// This should trigger the linter - comment flags are not allowed
	args1 := []string{"-t", "nat", "-A", "INPUT", "-m", "comment", "--comment", "test comment"}
	executeXTables("iptables", args1...) // want "iptables comment flags \\(-m comment, --comment\\) are not supported in gVisor environments"

	// This should also trigger the linter
	args2 := []string{"-t", "nat", "-A", "INPUT", "--comment", "standalone comment"}
	executeXTables("iptables", args2...) // want "iptables comment flags \\(-m comment, --comment\\) are not supported in gVisor environments"

	// This should NOT trigger the linter - no comment flags
	args3 := []string{"-t", "nat", "-A", "INPUT", "-p", "tcp", "-j", "ACCEPT"}
	executeXTables("iptables", args3...)

	// This should NOT trigger the linter - comment in other context
	args4 := []string{"-t", "nat", "-A", "INPUT", "--log-prefix", "comment-test"}
	executeXTables("iptables", args4...)
}

func executeXTables(binary string, args ...string) error {
	return nil
}