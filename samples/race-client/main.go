// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// race-client is a test program that mimics the connection-racing behavior of
// clients like .NET SqlClient (MultiSubnetFailover=True). It resolves a hostname
// via DNS, gets all A/AAAA records, and races TCP connections to all of them in
// parallel. The first successful connection wins; the rest are closed.
//
// This is useful for testing ztunnel's FIRST_HEALTHY_RACE connect strategy:
// without it, ztunnel may route the connection to an unhealthy backend (since
// the client only sees a single auto-allocated VIP). With FIRST_HEALTHY_RACE,
// ztunnel internally races connections on the client's behalf.
//
// Environment variables:
//
//	TARGET_URL    - The address to connect to, in host:port format (e.g. "sql.example.com:1433")
//	INTERVAL      - Time between connection attempts (default "300ms")
//	TIMEOUT       - Total timeout for DNS resolution + connection racing (default "10s")
//	SEND_DATA     - Optional data to send after connecting (e.g. a health-check payload)
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	target := os.Getenv("TARGET_URL")
	if target == "" {
		fmt.Fprintln(os.Stderr, "TARGET_URL environment variable is required (e.g. 'my-service.example.com:1433')")
		os.Exit(1)
	}

	interval := envDuration("INTERVAL", 300*time.Millisecond)
	timeout := envDuration("TIMEOUT", 10*time.Second)
	sendData := os.Getenv("SEND_DATA")

	fmt.Printf("race-client starting\n  target=%s interval=%s timeout=%s\n", target, interval, timeout)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on startup, then on each tick.
	for {
		raceConnect(ctx, target, timeout, sendData)

		select {
		case <-ctx.Done():
			fmt.Println("shutting down")
			return
		case <-ticker.C:
		}
	}
}

// raceConnect resolves all IPs for the target host and races TCP connections
// to every address in parallel, mimicking .NET SqlClient's ParallelConnect /
// MultiSubnetFailover behavior.
func raceConnect(ctx context.Context, target string, timeout time.Duration, sendData string) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		fmt.Printf("[%s] ERROR: invalid target %q: %v\n", ts(), target, err)
		return
	}

	// Step 1: DNS resolution - get ALL IPs, like the .NET client does.
	resolver := net.DefaultResolver
	resolveCtx, resolveCancel := context.WithTimeout(ctx, timeout)
	defer resolveCancel()

	ips, err := resolver.LookupHost(resolveCtx, host)
	if err != nil {
		fmt.Printf("[%s] ERROR: DNS lookup for %q failed: %v\n", ts(), host, err)
		return
	}
	fmt.Printf("[%s] DNS resolved %s -> %v\n", ts(), host, ips)

	if len(ips) == 0 {
		fmt.Printf("[%s] ERROR: no addresses returned for %q\n", ts(), host)
		return
	}

	// Step 2: Race TCP connections to ALL resolved IPs in parallel.
	// This is the core "happy eyeballs" / MultiSubnetFailover pattern.
	type result struct {
		conn net.Conn
		addr string
		err  error
	}

	connectCtx, connectCancel := context.WithTimeout(ctx, timeout)
	defer connectCancel()

	results := make(chan result, len(ips))
	var wg sync.WaitGroup

	dialer := &net.Dialer{}

	for _, ip := range ips {
		addr := net.JoinHostPort(ip, port)
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			conn, err := dialer.DialContext(connectCtx, "tcp", addr)
			results <- result{conn: conn, addr: addr, err: err}
		}(addr)
	}

	// Close the results channel when all goroutines complete.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Step 3: Pick the first successful connection; close the rest.
	var winner net.Conn
	var winnerAddr string
	var losers []net.Conn
	var errors []string

	for r := range results {
		if r.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", r.addr, r.err))
			continue
		}
		if winner == nil {
			winner = r.conn
			winnerAddr = r.addr
			// Cancel remaining dials.
			connectCancel()
		} else {
			// Another connection succeeded after we already picked a winner - close it.
			losers = append(losers, r.conn)
		}
	}

	// Close losing connections.
	for _, c := range losers {
		c.Close()
	}

	if winner == nil {
		fmt.Printf("[%s] FAIL: all connections failed:\n", ts())
		for _, e := range errors {
			fmt.Printf("  - %s\n", e)
		}
		return
	}

	defer winner.Close()
	fmt.Printf("[%s] SUCCESS: connected to %s (raced %d IPs, %d failed)\n",
		ts(), winnerAddr, len(ips), len(errors))

	// Step 4: Optionally send data and read response (simple health check).
	if sendData != "" {
		_ = winner.SetDeadline(time.Now().Add(timeout))
		_, err := winner.Write([]byte(sendData))
		if err != nil {
			fmt.Printf("[%s] ERROR: write to %s failed: %v\n", ts(), winnerAddr, err)
			return
		}

		buf := make([]byte, 4096)
		n, err := winner.Read(buf)
		if err != nil {
			fmt.Printf("[%s] ERROR: read from %s failed: %v\n", ts(), winnerAddr, err)
			return
		}
		fmt.Printf("[%s] RESPONSE from %s: %d bytes\n", ts(), winnerAddr, n)
	}
}

func ts() string {
	return time.Now().Format(time.RFC3339)
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: invalid %s=%q, using default %s\n", key, v, def)
		return def
	}
	return d
}
