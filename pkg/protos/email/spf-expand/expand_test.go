package spfexpand

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSPFBasics(t *testing.T) {
	tests := []struct {
		Record   string
		Expected *SPFRecord
	}{
		{"v=spf1 +all", &SPFRecord{Pass: SPFMechanism{All: true}}},
		{"v=spf1 -all", &SPFRecord{Fail: SPFMechanism{All: true}}},
		{"v=spf1 ~all", &SPFRecord{SoftFail: SPFMechanism{All: true}}},
		{"v=spf1 include:_spf.google.com ~all", &SPFRecord{Pass: SPFMechanism{Includes: map[string]*SPFRecord{"_spf.google.com": nil}}, SoftFail: SPFMechanism{All: true}}},
		{"v=spf1 mx include:_spf.google.com ~all", &SPFRecord{Pass: SPFMechanism{MX: true, Includes: map[string]*SPFRecord{"_spf.google.com": nil}}, SoftFail: SPFMechanism{All: true}}},
		{"v=spf1 ip4:1.1.1.1/20", &SPFRecord{Pass: SPFMechanism{IP4: []*net.IPNet{{IP: net.IPv4(1, 1, 1, 1), Mask: net.CIDRMask(20, 32)}}}}},
		{"v=spf1 ip4:1.1.1.1", &SPFRecord{Pass: SPFMechanism{IP4: []*net.IPNet{{IP: net.IPv4(1, 1, 1, 1), Mask: net.CIDRMask(32, 32)}}}}},
		{"v=spf1 ip6:1080::8:800:68.0.3.1/96", &SPFRecord{Pass: SPFMechanism{IP6: []*net.IPNet{{IP: net.ParseIP("1080::8:800:68.0.3.1"), Mask: net.CIDRMask(96, 128)}}}}},
		{"v=spf1 ip6:1080::8:800:68.0.3.1", &SPFRecord{Pass: SPFMechanism{IP6: []*net.IPNet{{IP: net.ParseIP("1080::8:800:68.0.3.1"), Mask: net.CIDRMask(128, 128)}}}}},
	}

	for _, tc := range tests {
		record, err := ParseSPF(tc.Record, "")
		if err != nil {
			t.Fatal(err)
		}

		//Clear loopback fields
		record.Original = ""
		record.Pass.r = nil
		record.Fail.r = nil
		record.Neutral.r = nil
		record.SoftFail.r = nil

		assert.Equal(t, tc.Expected, record, fmt.Sprintf("record: %s", tc.Record))
	}
}

func TestLookupRecord(t *testing.T) {
	record, err := GetSPF("google.com.au")
	if err != nil {
		t.Fatal(err)
	}

	assert.True(t, strings.HasPrefix(record, "v=spf1 "), record)
}

func TestPopulateMechanism(t *testing.T) {
	r, err := ParseSPF("v=spf1 mx", "google.com.au")
	if err != nil {
		t.Fatal(err)
	}

	if assert.NoError(t, r.Pass.PopulateRecords()) {
		assert.NotEmpty(t, r.Pass.MXIPs)
	}
}

func TestExpandIncludes(t *testing.T) {
	r, err := ParseSPF("v=spf1 include:google.com.au", "google.com.au")
	if err != nil {
		t.Fatal(err)
	}

	if assert.NoError(t, r.Pass.ExpandIncludes()) {
		assert.NotNil(t, r.Pass.Includes["google.com.au"])
	}
}

func TestCheckHost(t *testing.T) {
	addr := net.ParseIP("1.2.3.4")
	from := "test@example.com"
	r, err := CheckHost(addr, from)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, ResultFail, r)
}

func TestRedirects(t *testing.T) {
	tests := []struct {
		Record         string
		ShouldRedirect bool
		RedirectTo     string
	}{
		{"v=spf1 redirect=test.example.com", true, "test.example.com"},
		{"v=spf1 redirect=test.example.com ~all", false, "test.example.com"}, //redirect ignored due to 'all'
		{"v=spf1 ~all", false, ""},
	}

	for _, tc := range tests {
		record, err := ParseSPF(tc.Record, "")
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, tc.ShouldRedirect, record.ShouldRedirect())
		assert.Equal(t, tc.RedirectTo, record.Redirect)
	}
}
