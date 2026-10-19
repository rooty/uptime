package mailcheck

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"snippetbook/internal/domaincheck"
	"snippetbook/internal/model"
)

type Resolver interface {
	LookupMX(context.Context, string) ([]*net.MX, error)
	LookupTXT(context.Context, string) ([]string, error)
	LookupIP(context.Context, string, string) ([]net.IP, error)
}

type Checker struct {
	Resolver          Resolver
	Timeout           time.Duration
	BlacklistZones    []string
	BlacklistResolver BlacklistResolver
	MxToolbox         MxToolboxBlacklistResolver
	WebsiteCheck      func(context.Context, string, time.Duration) model.WebsiteResult
}

type BlacklistResolver interface {
	LookupBlacklist(context.Context, string) (listed bool, ttl uint32, err error)
}

type MxToolboxBlacklistResolver interface {
	LookupBlacklist(context.Context, string) (model.BlacklistResult, error)
}

var domainPattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))+$`)

var defaultBlacklistZones = []string{
	"b.barracudacentral.org",
	"zen.spamhaus.org",
	"bl.0spam.org",
	"rbl.0spam.org",
	"combined.abuse.ch",
	"dbl.abuse.ch",
	"exploit.abuse.ch",
	"spam.dnsbl.anonmails.de",
	"ips.backscatterer.org",
	"bl.blocklist.de",
	"dnsbl.calivent.com",
	"bogons.cymru.com",
	"tor.dan.me.uk",
	"torexit.dan.me.uk",
	"bl.drmx.org",
	"dnsbl.dronebl.org",
	"rbl.fabel.dk",
	"hil.habeas.com",
	"hil2.habeas.com",
	"hostkarma.junkemailfilter.com",
	"dnsbl.cobion.com",
	"icommon.org",
	"spamrbl.imp.ch",
	"wormrbl.imp.ch",
	"rbl.interserver.net",
	"sip.invaluement.com",
	"sip24.invaluement.com",
	"mail-abuse.blacklist.jippg.org",
	"dnsbl.kempt.net",
	"bl.konstant.no",
	"ubl.unsubscore.com",
	"bl.mailspike.net",
	"z.mailspike.net",
	"phishing.rbl.msrbl.net",
	"spam.rbl.msrbl.net",
	"relays.nether.net",
	"unsure.nether.net",
	"bl.nosolicitado.org",
	"psbl.surriel.com",
	"dyna.spamrats.com",
	"noptr.spamrats.com",
	"spam.spamrats.com",
	"rbl.jp",
	"all.s5h.net",
	"bl.schulte.org",
	"backscatter.spameatingmonkey.net",
	"bl.spameatingmonkey.net",
	"bl.score.senderscore.com",
	"dnsbl.services.net",
	"bl.spamcop.net",
	"dnsbl.sorbs.net",
	"bl.suomispam.net",
	"dnsbl.swinog.ch",
	"rbl.triumf.ca",
	"truncate.gbudb.net",
	"dnsbl-1.uceprotect.net",
	"dnsbl-2.uceprotect.net",
	"dnsbl-3.uceprotect.net",
	"smtp.dnsbl.sorbs.net",
	"dnsbl.zapbl.net",
	"dnsbl.spfbl.net",
}

var commonDKIMSelectors = []string{"default", "google", "selector1", "selector2", "k1", "dkim", "mail", "s1", "s2"}

func New(timeout time.Duration) *Checker {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Checker{
		Resolver:          net.DefaultResolver,
		Timeout:           timeout,
		BlacklistZones:    defaultBlacklistZones,
		BlacklistResolver: NewDNSBlacklistResolver(timeout),
		MxToolbox:         NewMxToolboxBlacklistResolver(strings.TrimSpace(os.Getenv("MXTOOLBOX_API_KEY")), timeout),
		WebsiteCheck:      checkWebsite,
	}
}

func (c *Checker) Check(ctx context.Context, domain, selector string) (model.MailCheckResult, error) {
	domain = normalizeDomain(domain)
	if !domainPattern.MatchString(domain) {
		return model.MailCheckResult{}, errors.New("domain must be a valid DNS name")
	}
	if c.Resolver == nil {
		c.Resolver = net.DefaultResolver
	}
	checkCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	result := model.MailCheckResult{
		MX:    checkMX(checkCtx, c.Resolver, domain),
		SPF:   checkSPF(checkCtx, c.Resolver, domain),
		DKIM:  checkDKIM(checkCtx, c.Resolver, domain, strings.TrimSpace(selector)),
		DMARC: checkDMARC(checkCtx, c.Resolver, domain),
	}
	result.Blacklists = c.checkBlacklists(checkCtx, result.MX.Records)
	result.MXTLS = checkMXTLS(checkCtx, result.MX.Records, c.Timeout)
	if c.WebsiteCheck != nil {
		result.Website = c.WebsiteCheck(checkCtx, domain, c.Timeout)
	}
	registration, registrationErr := (domaincheck.Client{}).Check(checkCtx, domain)
	result.Registration = registration
	if registrationErr != nil && result.Registration.Status == "" {
		result.Registration.Status = "error"
	}
	return result, nil
}

func (c *Checker) checkBlacklists(ctx context.Context, mxRecords []model.MXRecord) model.BlacklistResult {
	result := checkBlacklists(ctx, c.Resolver, c.BlacklistResolver, mxRecords, c.BlacklistZones)
	if c.MxToolbox == nil {
		return result
	}
	result.AdditionalProvider = "mxtoolbox"
	additional, err := checkBlacklistsWithMxToolbox(ctx, c.MxToolbox, mxRecords)
	if err != nil {
		result.AdditionalError = err.Error()
		return result
	}
	result.AdditionalChecked = additional.Checked
	result.AdditionalListed = additional.Listed
	result.AdditionalTimeout = additional.Timeout
	result.AdditionalResults = additional.Results
	return result
}

func checkBlacklistsWithMxToolbox(ctx context.Context, resolver MxToolboxBlacklistResolver, mxRecords []model.MXRecord) (model.BlacklistResult, error) {
	addresses := make([]string, 0)
	seen := make(map[string]bool)
	for _, mx := range mxRecords {
		for _, address := range mx.Addresses {
			if !seen[address] {
				seen[address] = true
				addresses = append(addresses, address)
			}
		}
	}
	if len(addresses) == 0 {
		return model.BlacklistResult{Status: "not_found", Provider: "mxtoolbox", Message: "no MX addresses available for blacklist checks"}, nil
	}
	result := model.BlacklistResult{Status: "clean", Provider: "mxtoolbox"}
	for _, address := range addresses {
		lookup, err := resolver.LookupBlacklist(ctx, address)
		if err != nil {
			return model.BlacklistResult{}, err
		}
		result.Results = append(result.Results, lookup.Results...)
		result.Checked += lookup.Checked
		result.Listed += lookup.Listed
		result.Timeout += lookup.Timeout
	}
	if result.Listed > 0 || result.Timeout > 0 {
		result.Status = "warning"
	}
	return result, nil
}

func checkMXTLS(ctx context.Context, records []model.MXRecord, timeout time.Duration) model.MXTLSResult {
	result := model.MXTLSResult{Status: "ok"}
	tlsContext := context.WithoutCancel(ctx)
	type tlsTask struct {
		host    string
		address string
		port    string
		tlsMode string
	}
	var tasks []tlsTask
	for _, record := range records {
		addresses := record.Addresses
		if len(addresses) == 0 {
			addresses = []string{""}
		}
		for _, address := range addresses {
			for _, endpoint := range []struct{ port, tlsMode string }{
				{port: "25", tlsMode: "SMTP STARTTLS"},
				{port: "587", tlsMode: "SMTP STARTTLS"},
				{port: "465", tlsMode: "SMTPS TLS"},
			} {
				tasks = append(tasks, tlsTask{host: record.Host, address: address, port: endpoint.port, tlsMode: endpoint.tlsMode})
			}
		}
	}
	result.Checked = len(tasks)
	result.Results = make([]model.MXTLSRecord, len(tasks))
	var waitGroup sync.WaitGroup
	for index, task := range tasks {
		waitGroup.Add(1)
		go func(index int, task tlsTask) {
			defer waitGroup.Done()
			item := model.MXTLSRecord{Host: task.host, Address: task.address, Port: task.port, TLSMode: task.tlsMode}
			connectHost := task.address
			if connectHost == "" {
				connectHost = task.host
			}
			state, err := checkSMTPTLS(tlsContext, task.host, connectHost, task.port, timeout)
			if err != nil {
				item.Status, item.Error = "error", err.Error()
			} else if len(state.PeerCertificates) == 0 {
				item.Status, item.Error = "error", "TLS peer returned no certificate"
			} else {
				certificate := state.PeerCertificates[0]
				item.Status, item.Issuer = "ok", certificate.Issuer.String()
				expiry := certificate.NotAfter.UTC()
				item.ExpiresAt = &expiry
				item.DaysRemaining = int(time.Until(expiry).Hours() / 24)
				if item.DaysRemaining <= 21 {
					item.Status = "expiring"
				}
			}
			result.Results[index] = item
		}(index, task)
	}
	waitGroup.Wait()
	for _, item := range result.Results {
		if item.Status == "ok" || item.Status == "expiring" {
			result.Valid++
		}
		if item.Status == "error" || item.Status == "expiring" {
			result.Status = "warning"
		}
	}
	if result.Checked == 0 {
		result.Status, result.Message = "not_found", "no MX servers available"
	}
	return result
}

func checkSMTPTLS(ctx context.Context, host, connectHost, port string, timeout time.Duration) (tls.ConnectionState, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	address := net.JoinHostPort(strings.TrimSuffix(connectHost, "."), port)
	deadline, hasDeadline := dialCtx.Deadline()
	if port == "465" {
		conn, err := (&tls.Dialer{Config: &tls.Config{ServerName: strings.TrimSuffix(host, "."), MinVersion: tls.VersionTLS12}}).DialContext(dialCtx, "tcp", address)
		if err != nil {
			return tls.ConnectionState{}, fmt.Errorf("TLS connect %s: %w", address, err)
		}
		tlsConn, ok := conn.(*tls.Conn)
		if !ok {
			closeErr := conn.Close()
			if closeErr != nil {
				return tls.ConnectionState{}, fmt.Errorf("TLS connection %s had unexpected type; close connection: %w", address, closeErr)
			}
			return tls.ConnectionState{}, fmt.Errorf("TLS connection %s had unexpected type", address)
		}
		state := tlsConn.ConnectionState()
		if closeErr := tlsConn.Close(); closeErr != nil {
			return tls.ConnectionState{}, fmt.Errorf("close TLS connection %s: %w", address, closeErr)
		}
		return state, nil
	}

	connection, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
	if err != nil {
		return tls.ConnectionState{}, fmt.Errorf("TCP connect %s: %w", address, err)
	}
	if hasDeadline {
		if err := connection.SetDeadline(deadline); err != nil {
			closeErr := connection.Close()
			if closeErr != nil {
				return tls.ConnectionState{}, fmt.Errorf("set SMTP deadline: %v; close connection: %w", err, closeErr)
			}
			return tls.ConnectionState{}, fmt.Errorf("set SMTP deadline: %w", err)
		}
	}
	client, err := smtp.NewClient(connection, strings.TrimSuffix(host, "."))
	if err != nil {
		closeErr := connection.Close()
		if closeErr != nil {
			return tls.ConnectionState{}, fmt.Errorf("SMTP greeting %s: %v; close connection: %w", address, err, closeErr)
		}
		return tls.ConnectionState{}, fmt.Errorf("SMTP greeting %s: %w", address, err)
	}
	defer func() {
		_ = client.Close()
	}()
	if err := client.Hello("localhost"); err != nil {
		return tls.ConnectionState{}, fmt.Errorf("SMTP EHLO %s: %w", address, err)
	}
	if err := client.StartTLS(&tls.Config{ServerName: strings.TrimSuffix(host, "."), MinVersion: tls.VersionTLS12}); err != nil {
		return tls.ConnectionState{}, fmt.Errorf("SMTP STARTTLS %s: %w", address, err)
	}
	state, ok := client.TLSConnectionState()
	if !ok {
		return tls.ConnectionState{}, fmt.Errorf("SMTP STARTTLS %s returned no TLS state", address)
	}
	return state, nil
}

func normalizeDomain(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

func NormalizeDomain(value string) string { return normalizeDomain(value) }

func ValidateDomain(value string) error {
	domain := normalizeDomain(value)
	if !domainPattern.MatchString(domain) {
		return errors.New("domain must be a valid DNS name")
	}
	return nil
}

func checkMX(ctx context.Context, resolver Resolver, domain string) model.MailSection {
	records, err := resolver.LookupMX(ctx, domain)
	if err != nil {
		return model.MailSection{Status: "not_found", Message: "MX records not found"}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Pref < records[j].Pref })
	result := model.MailSection{Status: "ok"}
	for _, record := range records {
		host := strings.TrimSuffix(strings.ToLower(record.Host), ".")
		item := model.MXRecord{Priority: record.Pref, Host: host}
		addresses, lookupErr := resolver.LookupIP(ctx, "ip", host)
		if lookupErr != nil {
			item.Error = lookupErr.Error()
			result.Status = "warning"
		} else {
			for _, address := range addresses {
				item.Addresses = append(item.Addresses, address.String())
			}
		}
		result.Records = append(result.Records, item)
	}
	if len(result.Records) == 0 {
		return model.MailSection{Status: "not_found", Message: "MX records not found"}
	}
	return result
}

func checkSPF(ctx context.Context, resolver Resolver, domain string) model.SPFResult {
	records, err := resolver.LookupTXT(ctx, domain)
	if err != nil {
		return model.SPFResult{Status: "not_found", Message: "SPF record not found"}
	}
	var spf []string
	for _, record := range records {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(record)), "v=spf1") {
			spf = append(spf, strings.TrimSpace(record))
		}
	}
	if len(spf) == 0 {
		return model.SPFResult{Status: "not_found", Message: "SPF record not found"}
	}
	result := model.SPFResult{Status: "ok", Record: spf[0]}
	if len(spf) > 1 {
		result.Status = "warning"
		result.Message = "multiple SPF records found"
	}
	result.Policy = spfPolicy(spf[0])
	result.Includes, result.DNSLookups = collectSPFMechanisms(ctx, resolver, domain, spf[0], 0, map[string]bool{})
	if result.DNSLookups > 10 {
		result.Status = "error"
		result.Message = "SPF DNS lookup limit exceeded"
	} else if result.DNSLookups >= 8 && result.Status == "ok" {
		result.Status = "warning"
		result.Message = "SPF is close to the DNS lookup limit"
	}
	if result.Policy == "+all" {
		result.Status = "warning"
		result.Message = "SPF uses permissive +all policy"
	}
	return result
}

func spfPolicy(record string) string {
	for _, token := range strings.Fields(record) {
		if strings.HasSuffix(token, "all") {
			return token
		}
	}
	return ""
}

func collectSPFMechanisms(ctx context.Context, resolver Resolver, domain, record string, depth int, visited map[string]bool) ([]string, int) {
	if depth > 10 || visited[domain] {
		return nil, 11
	}
	visited[domain] = true
	lookups := 0
	var includes []string
	for _, token := range strings.Fields(record) {
		token = strings.TrimLeft(token, "~?+-")
		switch {
		case strings.HasPrefix(token, "include:"):
			lookups++
			includeDomain := normalizeDomain(strings.TrimPrefix(token, "include:"))
			includes = append(includes, includeDomain)
			records, err := resolver.LookupTXT(ctx, includeDomain)
			if err == nil {
				for _, nested := range records {
					if strings.HasPrefix(strings.ToLower(strings.TrimSpace(nested)), "v=spf1") {
						nestedIncludes, nestedLookups := collectSPFMechanisms(ctx, resolver, includeDomain, nested, depth+1, visited)
						includes = append(includes, nestedIncludes...)
						lookups += nestedLookups
						break
					}
				}
			}
		case token == "a" || strings.HasPrefix(token, "a:") || strings.HasPrefix(token, "a/"):
			lookups++
		case token == "mx" || strings.HasPrefix(token, "mx:") || strings.HasPrefix(token, "mx/"):
			lookups++
		case token == "ptr" || strings.HasPrefix(token, "ptr:") || strings.HasPrefix(token, "ptr/"):
			lookups++
		case strings.HasPrefix(token, "exists:"):
			lookups++
		case strings.HasPrefix(token, "redirect="):
			lookups++
		}
	}
	return includes, lookups
}

func checkDKIM(ctx context.Context, resolver Resolver, domain, selector string) model.DKIMResult {
	selectors := []string{selector}
	if selector == "" {
		selectors = commonDKIMSelectors
	}
	result := model.DKIMResult{Status: "not_found", Message: "DKIM record not found"}
	for _, candidate := range selectors {
		if candidate == "" {
			continue
		}
		result.SelectorsTried = append(result.SelectorsTried, candidate)
		records, err := resolver.LookupTXT(ctx, candidate+"._domainkey."+domain)
		if err != nil || len(records) == 0 {
			continue
		}
		record := strings.Join(records, "")
		result.Selector = candidate
		result.Record = record
		if !strings.Contains(strings.ToLower(record), "p=") {
			result.Status = "error"
			result.Message = "DKIM public key is missing"
			return result
		}
		result.Status = "ok"
		result.Message = "DKIM selector found"
		return result
	}
	return result
}

func checkDMARC(ctx context.Context, resolver Resolver, domain string) model.DMARCResult {
	records, err := resolver.LookupTXT(ctx, "_dmarc."+domain)
	if err != nil || len(records) == 0 {
		return model.DMARCResult{Status: "not_found", Message: "DMARC record not found"}
	}
	record := strings.Join(records, "")
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(record)), "v=dmarc1") {
		return model.DMARCResult{Status: "error", Record: record, Message: "invalid DMARC record"}
	}
	result := model.DMARCResult{Status: "ok", Record: record, Policy: dmarcValue(record, "p")}
	result.RUA = dmarcValue(record, "rua")
	if result.Policy == "" {
		result.Status = "warning"
		result.Message = "DMARC policy is missing"
	} else if result.Policy == "none" {
		result.Status = "warning"
		result.Message = "DMARC policy is monitoring only"
	}
	return result
}

func dmarcValue(record, key string) string {
	for _, part := range strings.Split(record, ";") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) == 2 && strings.EqualFold(pair[0], key) {
			return strings.TrimSpace(pair[1])
		}
	}
	return ""
}

func checkBlacklists(ctx context.Context, resolver Resolver, blacklistResolver BlacklistResolver, mxRecords []model.MXRecord, zones []string) model.BlacklistResult {
	result := model.BlacklistResult{Status: "clean", Provider: "local_dnsbl"}
	if len(zones) == 0 {
		return result
	}
	type blacklistTask struct {
		ip   string
		zone string
	}
	var tasks []blacklistTask
	for _, mx := range mxRecords {
		for _, address := range mx.Addresses {
			for _, zone := range zones {
				tasks = append(tasks, blacklistTask{ip: address, zone: zone})
			}
		}
	}
	result.Checked = len(tasks)
	result.Results = make([]model.BlacklistEntry, len(tasks))
	semaphore := make(chan struct{}, 16)
	var waitGroup sync.WaitGroup
	for index, task := range tasks {
		waitGroup.Add(1)
		go func(index int, task blacklistTask) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				result.Results[index] = model.BlacklistEntry{IP: task.ip, List: blacklistName(task.zone), Zone: task.zone, Source: "local_dnsbl", Status: "timeout", Reason: ctx.Err().Error(), Error: ctx.Err().Error()}
				return
			}
			result.Results[index] = checkBlacklistEntry(ctx, resolver, blacklistResolver, task.ip, task.zone)
		}(index, task)
	}
	waitGroup.Wait()
	for _, entry := range result.Results {
		if entry.Listed {
			result.Listed++
		}
		if entry.Status == "timeout" {
			result.Timeout++
		}
	}
	if result.Listed > 0 || result.Timeout > 0 {
		result.Status = "warning"
	}
	if result.Checked == 0 {
		result.Status = "not_found"
		result.Message = "no MX addresses available for blacklist checks"
	}
	return result
}

func checkBlacklistEntry(ctx context.Context, resolver Resolver, blacklistResolver BlacklistResolver, address, zone string) model.BlacklistEntry {
	entry := model.BlacklistEntry{IP: address, List: blacklistName(zone), Zone: zone, Source: "local_dnsbl", Status: "ok", Reason: "Not listed"}
	applyBlacklistGuidance(&entry)
	query := reverseIP(address) + "." + zone
	started := time.Now()
	var listed bool
	var ttl uint32
	var err error
	if blacklistResolver != nil {
		listed, ttl, err = blacklistResolver.LookupBlacklist(ctx, query)
	} else {
		var answers []net.IP
		answers, err = resolver.LookupIP(ctx, "ip", query)
		listed = len(answers) > 0
	}
	entry.ResponseTimeMS = time.Since(started).Milliseconds()
	entry.TTL = ttl
	if listed {
		entry.Listed = true
		entry.Status = "listed"
		entry.Reason = "IP is listed"
	} else if err != nil {
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
			entry.Status = "timeout"
			entry.Reason = err.Error()
			entry.Error = err.Error()
		}
	}
	return entry
}

func blacklistName(zone string) string {
	names := map[string]string{
		"b.barracudacentral.org": "BARRACUDA", "zen.spamhaus.org": "Spamhaus ZEN", "bl.0spam.org": "0SPAM", "rbl.0spam.org": "0SPAM RBL",
		"combined.abuse.ch": "Abusix Mail Intelligence Blacklist", "dbl.abuse.ch": "Abusix Mail Intelligence Domain Blacklist", "exploit.abuse.ch": "Abusix Mail Intelligence Exploit list",
		"spam.dnsbl.anonmails.de": "Anonmails DNSBL", "ips.backscatterer.org": "BACKSCATTERER", "bl.blocklist.de": "BLOCKLIST.DE", "dnsbl.calivent.com": "CALIVENT",
		"bogons.cymru.com": "CYMRU BOGONS", "tor.dan.me.uk": "DAN TOR", "torexit.dan.me.uk": "DAN TOREXIT", "bl.drmx.org": "DRMX", "dnsbl.dronebl.org": "DRONE BL",
		"rbl.fabel.dk": "FABELSOURCES", "hil.habeas.com": "HIL", "hil2.habeas.com": "HIL2", "hostkarma.junkemailfilter.com": "Hostkarma Black",
		"dnsbl.cobion.com": "IBM DNS Blacklist", "icommon.org": "ICMFORBIDDEN", "spamrbl.imp.ch": "IMP SPAM", "wormrbl.imp.ch": "IMP WORM", "rbl.interserver.net": "INTERSERVER", "sip.invaluement.com": "ivmSIP", "sip24.invaluement.com": "ivmSIP24",
		"mail-abuse.blacklist.jippg.org": "JIPPG", "dnsbl.kempt.net": "KEMPTBL", "bl.konstant.no": "Konstant", "ubl.unsubscore.com": "LASHBACK", "bl.mailspike.net": "MAILSPIKE BL", "z.mailspike.net": "MAILSPIKE Z",
		"phishing.rbl.msrbl.net": "MSRBL Phishing", "spam.rbl.msrbl.net": "MSRBL Spam", "relays.nether.net": "NETHERRELAYS", "unsure.nether.net": "NETHERUNSURE", "bl.nosolicitado.org": "NoSolicitado",
		"psbl.surriel.com": "PSBL", "dyna.spamrats.com": "RATS Dyna", "noptr.spamrats.com": "RATS NoPtr", "spam.spamrats.com": "RATS Spam", "rbl.jp": "RBL JP", "all.s5h.net": "s5h.net",
		"bl.schulte.org": "SCHULTE", "backscatter.spameatingmonkey.net": "SEM BACKSCATTER", "bl.spameatingmonkey.net": "SEM BLACK", "bl.score.senderscore.com": "Sender Score Reputation Network",
		"dnsbl.services.net": "SERVICESNET", "bl.spamcop.net": "SPAMCOP", "dnsbl.sorbs.net": "SORBS", "bl.suomispam.net": "Suomispam Reputation", "dnsbl.swinog.ch": "SWINOG",
		"rbl.triumf.ca": "TRIUMF", "truncate.gbudb.net": "TRUNCATE", "dnsbl-1.uceprotect.net": "UCEPROTECTL1", "dnsbl-2.uceprotect.net": "UCEPROTECTL2", "dnsbl-3.uceprotect.net": "UCEPROTECTL3",
		"smtp.dnsbl.sorbs.net": "Woodys SMTP Blacklist", "dnsbl.zapbl.net": "ZapBL", "dnsbl.spfbl.net": "SPFBL DNSBL",
	}
	if name, ok := names[zone]; ok {
		return name
	}
	return zone
}

func applyBlacklistGuidance(entry *model.BlacklistEntry) {
	switch entry.Zone {
	case "zen.spamhaus.org":
		entry.InfoURL = "https://check.spamhaus.org/"
		entry.DelistURL = "https://check.spamhaus.org/"
		entry.DelistInstructions = "Проверьте причину листинга по IP, устраните источник спама или открытый relay и подайте запрос через страницу Spamhaus."
	case "bl.spamcop.net":
		entry.InfoURL = "https://www.spamcop.net/bl.shtml"
		entry.DelistURL = "https://www.spamcop.net/bl.shtml"
		entry.DelistInstructions = "Устраните жалобы и дождитесь автоматического снятия после прекращения поступления сообщений; проверьте IP на странице SpamCop."
	case "b.barracudacentral.org":
		entry.InfoURL = "https://www.barracudacentral.org/lookups"
		entry.DelistURL = "https://www.barracudacentral.org/rbl/removal-request"
		entry.DelistInstructions = "Проверьте IP, устраните причину блокировки и отправьте запрос на удаление через Barracuda Central."
	case "dnsbl.sorbs.net":
		entry.InfoURL = "https://www.sorbs.net/faq/"
		entry.DelistURL = "https://www.sorbs.net/faq/"
		entry.DelistInstructions = "Определите тип листинга по IP, устраните проблему и следуйте процедуре снятия в FAQ SORBS."
	}
}

func reverseIP(value string) string {
	ip := net.ParseIP(value)
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d", v4[3], v4[2], v4[1], v4[0])
	}
	if ip == nil {
		return ""
	}
	text := strings.ReplaceAll(ip.To16().String(), ":", "")
	parts := make([]string, 0, len(text))
	for index := len(text) - 1; index >= 0; index-- {
		parts = append(parts, string(text[index]))
	}
	return strings.Join(parts, ".")
}

func Overall(result model.MailCheckResult) (string, int) {
	statuses := []string{result.MX.Status, result.SPF.Status, result.DKIM.Status, result.DMARC.Status, result.Blacklists.Status, result.Website.Status}
	score := 100
	status := "ok"
	for _, section := range statuses {
		switch section {
		case "error":
			score -= 30
			status = "error"
		case "warning":
			score -= 15
			if status == "ok" {
				status = "warning"
			}
		case "not_found":
			score -= 20
			if status == "ok" {
				status = "warning"
			}
		}
	}
	if score < 0 {
		score = 0
	}
	return status, score
}
