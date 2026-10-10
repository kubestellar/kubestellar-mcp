package netguard

import (
	"net"
	"testing"
)

// FuzzIsBlockedIP enforces the IsBlockedIP contract as a property under
// fuzzing: for any string that parses as an IP address, the observed
// accept/reject decision must match an independently computed model built
// from the ranges IsBlockedIP documents (loopback, private, link-local,
// unspecified, CGNAT 100.64.0.0/10, cloud-metadata 169.254.169.254/32, and
// IETF reserved 192.0.0.0/24). IsBlockedIP is the single SSRF-blocklist
// predicate shared by every outbound-URL validator in this repo (see
// netguard.go's package doc), so property-based fuzzing here catches
// representation-level bypasses (e.g. IPv4-mapped IPv6 forms, or a future
// range-check regression) that the fixed table in netguard_test.go cannot.
//
// Companion to FuzzValidateNamespace (pkg/security/namespace) and
// FuzzSanitizeControlChars (pkg/security/sanitize). Add a matching matrix
// entry in .github/workflows/fuzz.yml so this target is exercised by the
// weekly Fuzz workflow.
func FuzzIsBlockedIP(f *testing.F) {
	seeds := []string{
		"127.0.0.1",
		"::1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"fd00::1",
		"169.254.1.1",
		"fe80::1",
		"0.0.0.0",
		"::",
		"100.64.0.0",
		"100.64.0.1",
		"100.127.255.255",
		"100.63.255.255",
		"100.128.0.0",
		"169.254.169.254",
		"192.0.0.1",
		"192.0.1.0",
		"8.8.8.8",
		"1.1.1.1",
		"2001:4860:4860::8888",
		"::ffff:169.254.169.254",
		"0:0:0:0:0:ffff:a9fe:a9fe",
		"::ffff:127.0.0.1",
		"",
		"not-an-ip",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		ip := net.ParseIP(s)
		if ip == nil {
			// Not a valid IP literal; IsBlockedIP's contract only covers
			// parsed net.IP values, so there is nothing to check.
			return
		}

		want := ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsUnspecified() ||
			cgnatNet.Contains(ip) || cloudMetaNet.Contains(ip) || ietfNet.Contains(ip)

		got := IsBlockedIP(ip)
		if got != want {
			t.Fatalf("IsBlockedIP(%q) = %v, want %v (loopback=%v private=%v linkLocal=%v unspecified=%v cgnat=%v cloudMeta=%v ietf=%v)",
				s, got, want,
				ip.IsLoopback(), ip.IsPrivate(), ip.IsLinkLocalUnicast(), ip.IsUnspecified(),
				cgnatNet.Contains(ip), cloudMetaNet.Contains(ip), ietfNet.Contains(ip))
		}

		// Representation invariant: whether an address is written as an
		// IPv4 literal or as its IPv4-mapped IPv6 form must not change the
		// blocked decision (a documented real-world SSRF bypass class).
		if v4 := ip.To4(); v4 != nil {
			mapped := net.ParseIP("::ffff:" + v4.String())
			if mapped != nil && IsBlockedIP(mapped) != got {
				t.Fatalf("IsBlockedIP diverges between %q (%v) and its IPv4-mapped IPv6 form %q (%v)",
					s, got, mapped.String(), IsBlockedIP(mapped))
			}
		}
	})
}
