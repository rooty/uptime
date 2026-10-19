package mailcheck

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"snippetbook/internal/model"
)

type fakeResolver struct {
	mx  map[string][]*net.MX
	txt map[string][]string
	ips map[string][]net.IP
}

type fakeBlacklistResolver struct {
	listed bool
	ttl    uint32
	err    error
}

func (f fakeBlacklistResolver) LookupBlacklist(context.Context, string) (bool, uint32, error) {
	return f.listed, f.ttl, f.err
}

func (f fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	return f.mx[name], nil
}

func (f fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	return f.txt[name], nil
}

func (f fakeResolver) LookupIP(_ context.Context, _, host string) ([]net.IP, error) {
	return f.ips[host], nil
}

func TestCheckDomainDiagnostics(t *testing.T) {
	resolver := fakeResolver{
		mx: map[string][]*net.MX{
			"example.com": {{Host: "mail.example.com.", Pref: 10}},
		},
		txt: map[string][]string{
			"example.com":                    {"v=spf1 include:_spf.example.net -all"},
			"_spf.example.net":               {"v=spf1 ip4:192.0.2.10 -all"},
			"default._domainkey.example.com": {"v=DKIM1; k=rsa; p=public-key"},
			"_dmarc.example.com":             {"v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com"},
		},
		ips: map[string][]net.IP{
			"mail.example.com": {net.ParseIP("192.0.2.10")},
		},
	}
	checker := New(time.Second)
	checker.Resolver = resolver
	checker.BlacklistZones = nil
	checker.WebsiteCheck = nil
	result, err := checker.Check(context.Background(), "Example.COM.", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.MX.Status != "ok" || result.SPF.Status != "ok" || result.DKIM.Status != "ok" || result.DMARC.Status != "ok" {
		t.Fatalf("result = %+v, want expected DNS statuses", result)
	}
	if result.SPF.DNSLookups != 1 || result.DKIM.Selector != "default" || len(result.DKIM.SelectorsTried) == 0 {
		t.Fatalf("result details = %+v", result)
	}
	status, score := Overall(result)
	if status != "ok" || score != 100 {
		t.Fatalf("overall = %s/%d, want ok/100", status, score)
	}
}

func TestCheckBlacklistsReturnsDetailedStatuses(t *testing.T) {
	mxRecords := []model.MXRecord{{Addresses: []string{"192.0.2.10"}}}
	zones := []string{"listed.example", "timeout.example", "clean.example"}
	resolver := sequenceBlacklistResolver{results: map[string]blacklistLookup{
		"listed.example":  {listed: true, ttl: 900},
		"timeout.example": {err: errors.New("i/o timeout")},
		"clean.example":   {},
	}}

	result := checkBlacklists(context.Background(), fakeResolver{}, &resolver, mxRecords, zones)
	if result.Checked != 3 || result.Listed != 1 || result.Timeout != 1 || result.Status != "warning" {
		t.Fatalf("unexpected blacklist summary: %+v", result)
	}
	if got := result.Results[0]; got.Status != "listed" || got.TTL != 900 || got.Reason != "IP is listed" {
		t.Fatalf("unexpected listed result: %+v", got)
	}
	if got := result.Results[1]; got.Status != "timeout" || got.Error != "i/o timeout" {
		t.Fatalf("unexpected timeout result: %+v", got)
	}
	if got := result.Results[2]; got.Status != "ok" || got.Reason != "Not listed" {
		t.Fatalf("unexpected clean result: %+v", got)
	}
}

func TestCheckSMTPTLSUsesSTARTTLSOnSMTPPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer connection.Close()
		if _, writeErr := fmt.Fprint(connection, "220 test SMTP\r\n"); writeErr != nil {
			serverDone <- writeErr
			return
		}
		reader := bufio.NewReader(connection)
		command, readErr := reader.ReadString('\n')
		if readErr != nil {
			serverDone <- readErr
			return
		}
		if !strings.HasPrefix(strings.ToUpper(command), "EHLO ") {
			serverDone <- fmt.Errorf("expected EHLO, got %q", command)
			return
		}
		_, writeErr := fmt.Fprint(connection, "250-test\r\n250 SIZE 100\r\n")
		serverDone <- writeErr
	}()

	_, err = checkSMTPTLS(context.Background(), "127.0.0.1", "127.0.0.1", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), time.Second)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("error = %v, want STARTTLS diagnostic", err)
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatal(serverErr)
	}
}

type blacklistLookup struct {
	listed bool
	ttl    uint32
	err    error
}

type sequenceBlacklistResolver struct {
	mu      sync.Mutex
	results map[string]blacklistLookup
}

func (r *sequenceBlacklistResolver) LookupBlacklist(_ context.Context, query string) (bool, uint32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result blacklistLookup
	for zone, candidate := range r.results {
		if strings.Contains(query, zone) {
			result = candidate
			break
		}
	}
	return result.listed, result.ttl, result.err
}

func TestCheckRejectsInvalidDomain(t *testing.T) {
	checker := New(time.Second)
	if _, err := checker.Check(context.Background(), "not a domain", ""); err == nil {
		t.Fatal("invalid domain was accepted")
	}
}
