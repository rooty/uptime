package mailcheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

type dnsBlacklistResolver struct {
	server  string
	timeout time.Duration
}

func NewDNSBlacklistResolver(timeout time.Duration) BlacklistResolver {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	server := "8.8.8.8:53"
	if config, err := dns.ClientConfigFromFile("/etc/resolv.conf"); err == nil && len(config.Servers) > 0 {
		server = net.JoinHostPort(config.Servers[0], config.Port)
	}
	return &dnsBlacklistResolver{server: server, timeout: timeout}
}

func (r *dnsBlacklistResolver) LookupBlacklist(ctx context.Context, query string) (bool, uint32, error) {
	if strings.TrimSpace(query) == "" {
		return false, 0, errors.New("empty DNSBL query")
	}
	message := new(dns.Msg)
	message.SetQuestion(dns.Fqdn(query), dns.TypeA)
	client := &dns.Client{Net: "udp", Timeout: r.timeout}
	response, _, err := client.ExchangeContext(ctx, message, r.server)
	if err != nil {
		return false, 0, err
	}
	if response.Rcode == dns.RcodeNameError || (response.Rcode == dns.RcodeSuccess && len(response.Answer) == 0) {
		return false, 0, nil
	}
	if response.Rcode != dns.RcodeSuccess {
		return false, 0, errors.New(dns.RcodeToString[response.Rcode])
	}
	var ttl uint32
	for _, answer := range response.Answer {
		if ttl == 0 || answer.Header().Ttl < ttl {
			ttl = answer.Header().Ttl
		}
	}
	return len(response.Answer) > 0, ttl, nil
}
