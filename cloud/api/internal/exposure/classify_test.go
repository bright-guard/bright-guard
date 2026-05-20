package exposure

import "testing"

func TestExposureClassify(t *testing.T) {
	cases := []struct {
		name    string
		address string
		state   string
	}{
		// Empty / garbage.
		{"empty", "", StateUnknown},
		{"whitespace", "   ", StateUnknown},
		{"garbage", "::::not a url::::", StateUnknown},
		{"only scheme", "https://", StateUnknown},

		// IPv4 loopback / private.
		{"loopback v4", "http://127.0.0.1:8080", StateInternal},
		{"loopback v4 high", "https://127.255.255.254", StateInternal},
		{"rfc1918 10", "http://10.0.0.4:9000", StateInternal},
		{"rfc1918 172.16", "http://172.16.5.1", StateInternal},
		{"rfc1918 192.168", "http://192.168.1.1:8443", StateInternal},
		{"link-local v4", "http://169.254.1.1", StateInternal},
		{"unspecified v4", "http://0.0.0.0:7000", StateInternal},

		// Public IPv4.
		{"public v4 cloudflare", "https://1.1.1.1", StatePublic},
		{"public v4 google dns", "https://8.8.8.8:443", StatePublic},
		{"public v4 random", "http://93.184.216.34/", StatePublic},

		// IPv6.
		{"loopback v6", "http://[::1]:8080", StateInternal},
		{"link-local v6", "http://[fe80::1]", StateInternal},
		{"unique-local v6", "http://[fd00::1]", StateInternal},
		{"public v6", "https://[2606:4700:4700::1111]", StatePublic},

		// DNS internal / cloud-internal suffixes.
		{"localhost", "http://localhost:3000", StateInternal},
		{"host docker", "http://host.docker.internal:9000", StateInternal},
		{"k8s svc", "http://my-svc.default.svc.cluster.local", StateInternal},
		{"k8s svc short", "http://my-svc.default.svc", StateInternal},
		{"cluster.local", "http://node1.cluster.local", StateInternal},
		{"mdns local", "http://printer.local", StateInternal},
		{"corp internal", "https://wiki.corp.internal", StateInternal},
		{"intranet", "http://intra.intranet", StateInternal},
		{"lan host", "http://nas.lan", StateInternal},
		{"single label", "http://buildbox", StateInternal},
		{"aws ec2 internal", "http://ip-10-0-0-5.ec2.internal", StateCloudInternal},
		{"aws compute internal", "http://ip-10-1-2-3.us-west-2.compute.internal", StateCloudInternal},
		{"aws zonal", "http://foo.us-east-1.internal.amazonaws.com", StateCloudInternal},
		{"azure cloudapp", "http://vm0.westus2.internal.cloudapp.net", StateCloudInternal},
		{"gcp internal", "http://node1.c.proj.internal.gcp.local", StateCloudInternal},

		// Public DNS.
		{"public dns example", "https://api.example.com/mcp", StatePublic},
		{"public dns subdomain", "https://mcp.openai.com", StatePublic},
		{"public dns with port", "https://api.anthropic.com:443/v1", StatePublic},
		{"bare host:port public", "api.example.com:8443", StatePublic},
		{"bare host:port private", "10.0.0.1:9000", StateInternal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := Classify(tc.address)
			if got != tc.state {
				t.Fatalf("Classify(%q) = %q (%s); want %q", tc.address, got, reason, tc.state)
			}
			if reason == "" {
				t.Fatalf("Classify(%q): empty reason", tc.address)
			}
		})
	}
}

// Caps test that the empty string and whitespace produce a state and reason
// distinct from a parseable-but-unknown address.
func TestExposureClassifyEmptyVsParseable(t *testing.T) {
	if state, _ := Classify(""); state != StateUnknown {
		t.Fatalf("empty: want %q got %q", StateUnknown, state)
	}
	if state, _ := Classify("https://1.1.1.1"); state != StatePublic {
		t.Fatalf("public ip: want %q got %q", StatePublic, state)
	}
}
