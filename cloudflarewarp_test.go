package cloudflarewarp_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	plugin "github.com/BetterCorp/cloudflarewarp"
	"github.com/BetterCorp/cloudflarewarp/ips"
)

func TestNew(t *testing.T) {
	cfg := plugin.CreateConfig()
	cfg.TrustIP = []string{"103.21.244.0/22", "172.18.0.1/32", "2405:b500::/32"}

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {})
	handler, err := plugin.New(ctx, next, cfg, "cloudflarewarp")
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		ipv6           bool
		expect400      bool
		trusted        bool
		remote         string
		desc           string
		cfConnectingIP string
		cfVisitor      string
		expected       string
		expectedScheme string
	}{
		{
			remote:         "103.21.244.23",
			desc:           "blank scheme",
			cfConnectingIP: "10.0.0.1",
			cfVisitor:      "",
			expected:       "10.0.0.1",
			expectedScheme: "",
			trusted:        true,
		},
		{
			remote:         "103.21.244.23",
			desc:           "https scheme",
			cfConnectingIP: "10.0.0.1",
			cfVisitor:      "{\"scheme\":\"https\"}",
			expected:       "10.0.0.1",
			expectedScheme: "https",
			trusted:        true,
		},
		{
			remote:         "10.0.1.20",
			desc:           "not trust",
			cfConnectingIP: "127.0.0.2",
			cfVisitor:      "",
			expected:       "",
			expectedScheme: "",
			trusted:        false,
		},
		{
			remote:         "10.0.1.20",
			desc:           "not trust ip4/6",
			cfConnectingIP: "1001:3984:3989::1",
			cfVisitor:      "",
			expected:       "",
			expectedScheme: "",
			trusted:        false,
		},
		{
			remote:         "1001:3984:3989::1",
			ipv6:           true,
			desc:           "not trust ip6/6",
			cfConnectingIP: "1001:3984:3989::1",
			cfVisitor:      "",
			expected:       "",
			expectedScheme: "",
			trusted:        false,
		},
		{
			remote:         "1001:3984:3989::1",
			ipv6:           true,
			desc:           "not trust ip6/4",
			cfConnectingIP: "10.0.1.20",
			cfVisitor:      "",
			expected:       "",
			expectedScheme: "",
			trusted:        false,
		},
		{
			remote:         "10.0.2",
			desc:           "wrong IP format",
			cfConnectingIP: "10.0.0.1",
			cfVisitor:      "",
			expected:       "",
			expectedScheme: "",
			expect400:      true,
			trusted:        false,
		},
		{
			remote:         "10.0.300.20",
			desc:           "wrong IP address",
			cfConnectingIP: "10.0.0.1",
			cfVisitor:      "",
			expected:       "",
			expectedScheme: "",
			expect400:      true,
			trusted:        false,
		},
		{
			remote:         "103.21.244.23",
			desc:           "forward",
			cfConnectingIP: "10.0.0.1",
			cfVisitor:      "",
			expected:       "10.0.0.1",
			expectedScheme: "",
			trusted:        true,
		},
		{
			remote:         "172.18.0.1",
			desc:           "forward",
			cfConnectingIP: "10.0.0.1",
			cfVisitor:      "",
			expected:       "10.0.0.1",
			expectedScheme: "",
			trusted:        true,
		},
	}
	for _, test := range testCases {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.ipv6 == true {
				req.RemoteAddr = "[" + test.remote + "]:36001"
			} else {
				req.RemoteAddr = test.remote + ":36001"
			}
			req.Header.Set("X-Real-Ip", test.remote)
			req.Header.Set("Cf-Connecting-IP", test.cfConnectingIP)
			req.Header.Set("Cf-Visitor", test.cfVisitor)

			handler.ServeHTTP(recorder, req)

			if recorder.Result().StatusCode == http.StatusBadRequest {
				if test.expect400 == true {
					return
				}
			}
			if recorder.Result().StatusCode != http.StatusOK {
				t.Errorf("invalid response: " + strconv.Itoa(recorder.Result().StatusCode))
				return
			}

			if test.trusted {
				assertHeader(t, req, "X-Is-Trusted", "yes")
				assertHeader(t, req, "X-Real-Ip", test.expected)
			} else {
				assertHeader(t, req, "X-Is-Trusted", "no")
				assertHeader(t, req, "X-Real-Ip", test.remote)
			}
			assertHeader(t, req, "X-Forwarded-For", test.expected)
			assertHeader(t, req, "X-Forwarded-Proto", test.expectedScheme)
		})
	}
}

func TestError(t *testing.T) {
	cfg := plugin.CreateConfig()
	cfg.TrustIP = []string{"103.21.244.0"}

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {})
	_, err := plugin.New(ctx, next, cfg, "cloudflarewarp")
	if err == nil {
		t.Fatalf("expected error, got none")
	}
}

func TestClientIPValidationAndFallback(t *testing.T) {
	const trustedPeer = "192.0.2.1"
	cfg := &plugin.Config{TrustIP: []string{trustedPeer + "/32"}, DisableDefaultCFIPs: true}
	tests := []struct {
		name       string
		clientIPs  []string
		wantStatus string
		wantIP     string
	}{
		{
			name: "valid IPv4", clientIPs: []string{"203.0.113.5"},
			wantStatus: "yes", wantIP: "203.0.113.5",
		},
		{
			name: "canonical IPv6", clientIPs: []string{"2001:0DB8:0000:0000:0000:0000:0000:0005"},
			wantStatus: "yes", wantIP: "2001:db8::5",
		},
		{
			name: "missing", wantStatus: "no", wantIP: trustedPeer,
		},
		{
			name: "blank", clientIPs: []string{""},
			wantStatus: "danger", wantIP: trustedPeer,
		},
		{
			name: "invalid address", clientIPs: []string{"unavailable"},
			wantStatus: "danger", wantIP: trustedPeer,
		},
		{
			name: "duplicate header values", clientIPs: []string{"203.0.113.5", "203.0.113.5"},
			wantStatus: "danger", wantIP: trustedPeer,
		},
		{
			name: "address list", clientIPs: []string{"203.0.113.5, 203.0.113.6"},
			wantStatus: "danger", wantIP: trustedPeer,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			incoming := http.Header{
				"X-Real-Ip":         {"198.51.100.5"},
				"X-Forwarded-For":   {"198.51.100.5", "198.51.100.6"},
				"X-Forwarded-Proto": {"http"},
				"X-Is-Trusted":      {"no"},
				"Cf-Visitor":        {`{"scheme":"https"}`},
			}
			for _, value := range test.clientIPs {
				incoming.Add("CF-Connecting-IP", value)
			}
			outgoing := downstreamHeaders(t, cfg, trustedPeer+":36001", incoming)
			assertHeaderValues(t, outgoing, "X-Is-Trusted", test.wantStatus)
			assertHeaderValues(t, outgoing, "X-Real-Ip", test.wantIP)
			assertHeaderValues(t, outgoing, "X-Forwarded-Proto", "https")
			if test.wantStatus == "yes" {
				assertHeaderValues(t, outgoing, "X-Forwarded-For", test.wantIP)
				assertHeaderValues(t, outgoing, "CF-Connecting-IP", test.clientIPs...)
			} else {
				assertHeaderValues(t, outgoing, "X-Forwarded-For")
				assertHeaderValues(t, outgoing, "CF-Connecting-IP")
			}
		})
	}
}

func TestVisitorSchemeValidation(t *testing.T) {
	cfg := &plugin.Config{TrustIP: []string{"192.0.2.1/32"}, DisableDefaultCFIPs: true}
	tests := []struct {
		name    string
		values  []string
		want    string
		keepRaw bool
	}{
		{name: "valid http", values: []string{`{"scheme":"http"}`}, want: "http", keepRaw: true},
		{name: "missing", want: "https"},
		{name: "empty", values: []string{""}, want: "https"},
		{name: "malformed JSON", values: []string{"{"}, want: "https"},
		{name: "missing scheme", values: []string{"{}"}, want: "https"},
		{name: "non-string scheme", values: []string{`{"scheme":true}`}, want: "https"},
		{name: "unsupported scheme", values: []string{`{"scheme":"ftp"}`}, want: "https"},
		{name: "duplicate header values", values: []string{`{"scheme":"http"}`, `{"scheme":"http"}`}, want: "https"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const visitorIP = "203.0.113.5"
			incoming := http.Header{
				"Cf-Connecting-Ip":  {visitorIP},
				"X-Forwarded-Proto": {"https"},
			}
			for _, value := range test.values {
				incoming.Add("CF-Visitor", value)
			}
			outgoing := downstreamHeaders(t, cfg, "192.0.2.1:36001", incoming)
			assertHeaderValues(t, outgoing, "X-Is-Trusted", "yes")
			assertHeaderValues(t, outgoing, "X-Real-Ip", visitorIP)
			assertHeaderValues(t, outgoing, "X-Forwarded-For", visitorIP)
			assertHeaderValues(t, outgoing, "X-Forwarded-Proto", test.want)
			if test.keepRaw {
				assertHeaderValues(t, outgoing, "CF-Visitor", test.values...)
			} else {
				assertHeaderValues(t, outgoing, "CF-Visitor")
			}
		})
	}
}

func TestUntrustedPeerFallback(t *testing.T) {
	cfg := &plugin.Config{TrustIP: []string{"192.0.2.1/32"}, DisableDefaultCFIPs: true}
	incoming := http.Header{
		"Cf-Connecting-Ip":  {"203.0.113.5"},
		"Cf-Visitor":        {`{"scheme":"https"}`},
		"X-Forwarded-For":   {"198.51.100.5"},
		"X-Forwarded-Proto": {"http"},
		"X-Is-Trusted":      {"yes"},
		"X-Real-Ip":         {"198.51.100.5"},
	}
	outgoing := downstreamHeaders(t, cfg, "[2001:db8::1]:36001", incoming)
	assertHeaderValues(t, outgoing, "X-Is-Trusted", "no")
	assertHeaderValues(t, outgoing, "X-Real-Ip", "2001:db8::1")
	assertHeaderValues(t, outgoing, "X-Forwarded-For")
	assertHeaderValues(t, outgoing, "X-Forwarded-Proto", "http")
	assertHeaderValues(t, outgoing, "CF-Connecting-IP")
	assertHeaderValues(t, outgoing, "CF-Visitor")
}

func TestBundledCloudflareTrust(t *testing.T) {
	defaultConfig := plugin.CreateConfig()
	customConfig := &plugin.Config{
		DisableDefaultCFIPs: true,
		TrustIP:             []string{"192.0.2.1/32", "2001:db8::1/128"},
	}
	visitor := http.Header{"Cf-Connecting-Ip": {"203.0.113.5"}}
	var hasIPv4, hasIPv6 bool
	for _, cidr := range ips.CFIPs() {
		address, _, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		if address.To4() != nil {
			hasIPv4 = true
		} else {
			hasIPv6 = true
		}
		t.Run(cidr, func(t *testing.T) {
			remote := net.JoinHostPort(address.String(), "36001")
			trusted := downstreamHeaders(t, defaultConfig, remote, visitor.Clone())
			assertHeaderValues(t, trusted, "X-Is-Trusted", "yes")
			assertHeaderValues(t, trusted, "X-Real-Ip", "203.0.113.5")

			untrusted := downstreamHeaders(t, customConfig, remote, visitor.Clone())
			assertHeaderValues(t, untrusted, "X-Is-Trusted", "no")
			assertHeaderValues(t, untrusted, "X-Real-Ip", address.String())
		})
	}
	if !hasIPv4 || !hasIPv6 {
		t.Fatal("expected bundled IPv4 and IPv6 ranges")
	}

	for _, address := range []string{"192.0.2.1", "2001:db8::1"} {
		t.Run("explicit "+address, func(t *testing.T) {
			remote := net.JoinHostPort(address, "36001")
			trusted := downstreamHeaders(t, customConfig, remote, visitor.Clone())
			assertHeaderValues(t, trusted, "X-Is-Trusted", "yes")
			assertHeaderValues(t, trusted, "X-Real-Ip", "203.0.113.5")
		})
	}
}

func downstreamHeaders(t *testing.T, cfg *plugin.Config, remote string, incoming http.Header) http.Header {
	t.Helper()
	var outgoing http.Header
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		outgoing = req.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	req.RemoteAddr = remote
	req.Header = incoming
	handler, err := plugin.New(req.Context(), next, cfg, "cloudflarewarp")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("next handler did not respond (status %d)", recorder.Code)
	}
	return outgoing
}

func assertHeaderValues(t *testing.T, headers http.Header, key string, want ...string) {
	t.Helper()
	if got := headers.Values(key); !slices.Equal(got, want) {
		t.Errorf("%s = %q, want %q", key, got, want)
	}
}

func assertHeader(t *testing.T, req *http.Request, key, expected string) {
	t.Helper()

	if req.Header.Get(key) != expected {
		t.Errorf("invalid header(%s) value: %s", key, req.Header.Get(key))
	}
}
