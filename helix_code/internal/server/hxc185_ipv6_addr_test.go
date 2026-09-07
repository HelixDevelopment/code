// §11.4.115 RED-baseline-on-the-broken-artifact guard for HXC-185 at
// internal/server/server.go (http.Server.Addr composition).
//
// DEFECT (captured on the pre-fix artifact, go1.26.4):
//
//	server.go  Addr: fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port)
//
// cfg.Server.Address is operator-supplied and can be a bare IPv6 literal.
// The naive join yields "::1:8080", which net.Listen — the call
// http.Server.ListenAndServe makes with exactly this Addr — rejects:
//
//	net.Listen("tcp", "::1:0")   -> listen tcp: address ::1:0: too many colons in address
//	net.Listen("tcp", "[::1]:0") -> ok
//
// so an IPv6-configured server never bound at all.
//
// This drives the REAL exported constructor (server.New) and then performs a
// REAL net.Listen on the Addr it produced, asserting a socket actually binds
// at the expected IPv6 address (positive sink-side evidence, §11.4.69).
package server

import (
	"net"
	"os"
	"strconv"
	"testing"

	"dev.helix.code/internal/config"
)

func hxc185RedMode() bool { return os.Getenv("RED_MODE") == "1" }

func TestServerAddr_IPv6_Listens(t *testing.T) {
	// Confirm IPv6 loopback is usable before asserting anything about it.
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("SKIP-OK HXC-185: IPv6 loopback unavailable on this host: %v", err)
	}
	_ = probe.Close()

	cases := []struct {
		name      string
		address   string
		redBroken bool
	}{
		{"ipv4_loopback", "127.0.0.1", false},
		{"bare_ipv6_loopback", "::1", true},
		{"already_bracketed_ipv6_loopback", "[::1]", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.Address = tc.address
			cfg.Server.Port = 0 // ephemeral

			srv := newTestServer(t, cfg, nil, nil)
			addr := srv.server.Addr

			if hxc185RedMode() {
				if !tc.redBroken {
					t.Skipf("SKIP-OK HXC-185: pre-fix composition already correct for %q", tc.address)
				}
				ln, lerr := net.Listen("tcp", addr)
				if lerr == nil {
					_ = ln.Close()
					t.Fatalf("RED_MODE=1: expected net.Listen(%q) to be REJECTED, but it bound — "+
						"defect did not reproduce", addr)
				}
				t.Logf("defect reproduced (captured evidence): net.Listen(%q) -> %v", addr, lerr)
				return
			}

			// The Addr must be a valid listen address AND must actually bind.
			ln, lerr := net.Listen("tcp", addr)
			if lerr != nil {
				t.Fatalf("server.New produced Addr %q for configured address %q, which net.Listen "+
					"REJECTS: %v — ListenAndServe could never bind", addr, tc.address, lerr)
			}
			defer func() { _ = ln.Close() }()

			boundHost, boundPort, serr := net.SplitHostPort(ln.Addr().String())
			if serr != nil {
				t.Fatalf("bound address %q does not split: %v", ln.Addr().String(), serr)
			}
			wantHost := tc.address
			if len(wantHost) >= 2 && wantHost[0] == '[' && wantHost[len(wantHost)-1] == ']' {
				wantHost = wantHost[1 : len(wantHost)-1]
			}
			if boundHost != wantHost {
				t.Fatalf("bound host = %q, want %q (the server bound a DIFFERENT interface than configured)",
					boundHost, wantHost)
			}
			if _, cerr := strconv.Atoi(boundPort); cerr != nil {
				t.Fatalf("bound port %q is not numeric: %v", boundPort, cerr)
			}
			t.Logf("positive sink-side evidence: configured %q -> Addr %q -> bound socket at %q",
				tc.address, addr, ln.Addr().String())
		})
	}
}
