// Package geo enriches monitoring services without participating in health checks.
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

const RefreshInterval = 24 * time.Hour

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type Worker struct {
	Store    *store.DB
	Resolver Resolver
	Client   *http.Client
	BaseURL  string
	Now      func() time.Time
}

func New(db *store.DB) *Worker {
	return &Worker{
		Store: db, Resolver: net.DefaultResolver,
		Client:  &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		BaseURL: "http://ip-api.com/json/", Now: time.Now,
	}
}

// Hostname never exposes credentials, URL paths or query strings to the provider.
func Hostname(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}

var specialNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b::/96"),
}

func PublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range specialNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func Stale(location model.IPGeolocation, now time.Time) bool {
	return location.LastSuccessAt != nil && (location.Error != "" || !now.Before(location.LastSuccessAt.Add(RefreshInterval)))
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := w.Step(ctx); err != nil && ctx.Err() == nil {
			log.Printf("geolocation: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Step resolves due hosts and performs at most one provider request. The shared
// limiter is persisted so restart cannot bypass a provider-imposed cooldown.
func (w *Worker) Step(ctx context.Context) error {
	services, err := w.Store.ListServices()
	if err != nil {
		return err
	}
	bindings, err := w.Store.ServiceLocations()
	if err != nil {
		return err
	}
	cache, err := w.Store.IPGeolocations()
	if err != nil {
		return err
	}
	nextProvider, err := w.Store.GeoProviderNextAttempt()
	if err != nil {
		return err
	}
	active := make(map[string]bool)
	// Resolve identical hostnames once per pass, including newly-created services.
	resolved := make(map[string]model.ServiceLocation)
	for _, service := range services {
		binding := bindings[service.ID]
		if binding.Hostname == Hostname(service.URL) && w.Now().Before(binding.NextAttemptAt) {
			resolved[binding.Hostname] = binding
		}
	}
	for _, service := range services {
		if err := ctx.Err(); err != nil {
			return err
		}
		host := Hostname(service.URL)
		binding, exists := bindings[service.ID]
		if !exists || binding.Hostname != host {
			binding = model.ServiceLocation{ServiceID: service.ID, Hostname: host}
		}
		if !w.Now().Before(binding.NextAttemptAt) {
			if shared, ok := resolved[host]; ok {
				if shared.Error != "" {
					// Preserve this service's last known addresses on DNS failure.
					binding.Error, binding.NextAttemptAt = shared.Error, shared.NextAttemptAt
				} else {
					binding = shared
					binding.ServiceID = service.ID
				}
			} else {
				binding = w.resolve(ctx, binding)
				resolved[host] = binding
			}
			if err := w.Store.SaveServiceLocation(binding); err != nil {
				return err
			}
		}
		for _, ip := range binding.IPs {
			active[ip] = true
		}
	}
	if w.Now().Before(nextProvider) {
		return nil
	}
	// Oldest due entry first prevents a failing address starving other services.
	var due []model.IPGeolocation
	for ip := range active {
		address, err := netip.ParseAddr(ip)
		if err != nil || !PublicIP(address) {
			continue
		}
		location := cache[ip]
		location.IP = ip
		if !w.Now().Before(location.NextAttemptAt) {
			due = append(due, location)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if due[i].NextAttemptAt.Equal(due[j].NextAttemptAt) {
			return due[i].IP < due[j].IP
		}
		return due[i].NextAttemptAt.Before(due[j].NextAttemptAt)
	})
	if len(due) == 0 {
		return nil
	}
	// Reserve a slot before sending, including when transport fails.
	if err := w.Store.SetGeoProviderNextAttempt(w.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	location, cooldown := w.lookup(ctx, due[0])
	next := w.Now().Add(cooldown)
	if location.NextAttemptAt.Before(next) {
		location.NextAttemptAt = next
	}
	// Persist the provider's restriction even if saving its payload later fails.
	if err := w.Store.SetGeoProviderNextAttempt(next); err != nil {
		return err
	}
	return w.Store.SaveIPGeolocation(location)
}

func (w *Worker) resolve(ctx context.Context, binding model.ServiceLocation) model.ServiceLocation {
	now := w.Now()
	var addresses []netip.Addr
	var err error
	if binding.Hostname == "" {
		err = fmt.Errorf("endpoint: hostname отсутствует")
	} else if ip, parseErr := netip.ParseAddr(binding.Hostname); parseErr == nil {
		addresses = []netip.Addr{ip}
	} else {
		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		addresses, err = w.Resolver.LookupNetIP(lookupCtx, "ip", binding.Hostname)
		cancel()
	}
	if err == nil && len(addresses) == 0 {
		err = fmt.Errorf("записи A/AAAA не найдены")
	}
	if err != nil {
		binding.Error = fmt.Sprintf("DNS lookup %s: %v", binding.Hostname, err)
		binding.NextAttemptAt = now.Add(5 * time.Minute)
		return binding
	}
	ips := make(map[string]bool)
	for _, ip := range addresses {
		if ip.IsValid() {
			ips[ip.Unmap().String()] = true
		}
	}
	binding.IPs = make([]string, 0, len(ips))
	for ip := range ips {
		binding.IPs = append(binding.IPs, ip)
	}
	sort.Strings(binding.IPs)
	binding.Error = ""
	binding.ResolvedAt = now
	binding.NextAttemptAt = now.Add(time.Hour)
	return binding
}

func (w *Worker) lookup(ctx context.Context, previous model.IPGeolocation) (model.IPGeolocation, time.Duration) {
	now := w.Now()
	previous.LastAttemptAt = now
	previous.Failures++
	backoff := time.Minute
	for i := 1; i < previous.Failures && backoff < time.Hour; i++ {
		backoff *= 2
	}
	if backoff > time.Hour {
		backoff = time.Hour
	}
	previous.NextAttemptAt = now.Add(backoff)
	previous.Status = "error"
	fail := func(err error) (model.IPGeolocation, time.Duration) {
		previous.Error = fmt.Sprintf("ip-api.com IP %s: %v", previous.IP, err)
		return previous, 2 * time.Second
	}
	endpoint := strings.TrimRight(w.BaseURL, "/") + "/" + url.PathEscape(previous.IP)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?fields=status,message,query,country,countryCode,regionName,city,lat,lon,timezone,isp,org,as", nil)
	if err != nil {
		return fail(fmt.Errorf("request: %w", err))
	}
	resp, err := w.Client.Do(req)
	if err != nil {
		return fail(fmt.Errorf("HTTP request: %w", err))
	}
	defer resp.Body.Close()
	cooldown := 2 * time.Second
	if resp.Header.Get("X-Rl") == "0" || resp.StatusCode == http.StatusTooManyRequests {
		seconds, err := strconv.Atoi(resp.Header.Get("X-Ttl"))
		if err != nil || seconds < 1 {
			seconds = 60
		}
		if seconds > 86400 {
			seconds = 86400
		}
		cooldown = time.Duration(seconds)*time.Second + time.Second
	}
	if resp.StatusCode != http.StatusOK {
		location, _ := fail(fmt.Errorf("HTTP status %d", resp.StatusCode))
		if location.NextAttemptAt.Before(now.Add(cooldown)) {
			location.NextAttemptAt = now.Add(cooldown)
		}
		return location, cooldown
	}
	var data struct {
		model.IPGeolocation
		Query   string `json:"query"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&data); err != nil {
		location, _ := fail(fmt.Errorf("JSON response: %w", err))
		return location, cooldown
	}
	if data.Status != "success" {
		location, _ := fail(fmt.Errorf("response status=%s: %s", data.Status, data.Message))
		return location, cooldown
	}
	address, err := netip.ParseAddr(data.Query)
	if err != nil || address.Unmap().String() != previous.IP {
		location, _ := fail(fmt.Errorf("response IP mismatch: %q", data.Query))
		return location, cooldown
	}
	if data.Latitude == nil || data.Longitude == nil || *data.Latitude < -90 || *data.Latitude > 90 || *data.Longitude < -180 || *data.Longitude > 180 {
		location, _ := fail(fmt.Errorf("response: координаты отсутствуют или вне диапазона"))
		return location, cooldown
	}
	// Only copy provider-owned fields; never accept cache metadata from HTTP.
	location := model.IPGeolocation{
		IP: previous.IP, Status: "success", Country: data.Country, CountryCode: data.CountryCode,
		Region: data.Region, City: data.City, Latitude: data.Latitude, Longitude: data.Longitude,
		Timezone: data.Timezone, ISP: data.ISP, Organization: data.Organization, AS: data.AS,
		LastAttemptAt: now, LastSuccessAt: &now, NextAttemptAt: now.Add(RefreshInterval),
	}
	return location, cooldown
}
