package model

import "time"

const MailCheckHistoryLimit = 2

type MailCheck struct {
	ID           int64           `json:"id"`
	Domain       string          `json:"domain"`
	DKIMSelector string          `json:"dkim_selector,omitempty"`
	Status       string          `json:"status"`
	Score        int             `json:"score"`
	CheckedAt    time.Time       `json:"checked_at"`
	Result       MailCheckResult `json:"result"`
	AIReport     string          `json:"ai_report,omitempty"`
	AIError      string          `json:"ai_error,omitempty"`
	AIModel      string          `json:"ai_model,omitempty"`
	AIUpdatedAt  *time.Time      `json:"ai_updated_at,omitempty"`
}

type MailMonitor struct {
	ID                      int64      `json:"id"`
	Domain                  string     `json:"domain"`
	DKIMSelector            string     `json:"dkim_selector,omitempty"`
	CheckInterval           string     `json:"check_interval"`
	Enabled                 bool       `json:"enabled"`
	LastStatus              string     `json:"last_status,omitempty"`
	LastScore               int        `json:"last_score,omitempty"`
	LastChecked             *time.Time `json:"last_checked,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	RegistrationStatus      string     `json:"registration_status,omitempty"`
	RegistrationExpiresAt   *time.Time `json:"registration_expires_at,omitempty"`
	RegistrationFingerprint string     `json:"registration_fingerprint,omitempty"`
}

type MailCheckResult struct {
	MX           MailSection        `json:"mx"`
	SPF          SPFResult          `json:"spf"`
	DKIM         DKIMResult         `json:"dkim"`
	DMARC        DMARCResult        `json:"dmarc"`
	Blacklists   BlacklistResult    `json:"blacklists"`
	Website      WebsiteResult      `json:"website"`
	Registration DomainRegistration `json:"registration"`
	MXTLS        MXTLSResult        `json:"mx_tls"`
}

type WebsiteResult struct {
	Status      string            `json:"status"`
	Different   bool              `json:"different,omitempty"`
	Differences []string          `json:"differences,omitempty"`
	Message     string            `json:"message,omitempty"`
	Results     []WebsiteEndpoint `json:"results,omitempty"`
}

type WebsiteEndpoint struct {
	URL                string `json:"url"`
	FinalURL           string `json:"final_url,omitempty"`
	Status             string `json:"status"`
	HTTPStatus         int    `json:"http_status,omitempty"`
	ContentLength      int64  `json:"content_length,omitempty"`
	ContentFingerprint string `json:"content_fingerprint,omitempty"`
	Error              string `json:"error,omitempty"`
}

type DomainRegistration struct {
	Status        string               `json:"status"`
	Registrar     string               `json:"registrar,omitempty"`
	ExpiresAt     *time.Time           `json:"expires_at,omitempty"`
	DaysRemaining int                  `json:"days_remaining,omitempty"`
	Changed       bool                 `json:"changed,omitempty"`
	Changes       []RegistrationChange `json:"changes,omitempty"`
	Fingerprint   string               `json:"fingerprint,omitempty"`
	Message       string               `json:"message,omitempty"`
}

type RegistrationChange struct {
	Field    string `json:"field"`
	Previous string `json:"previous"`
	Current  string `json:"current"`
}

type MXTLSResult struct {
	Status  string        `json:"status"`
	Checked int           `json:"checked"`
	Valid   int           `json:"valid"`
	Results []MXTLSRecord `json:"results,omitempty"`
	Message string        `json:"message,omitempty"`
}

type MXTLSRecord struct {
	Host          string     `json:"host"`
	Address       string     `json:"address,omitempty"`
	Port          string     `json:"port"`
	TLSMode       string     `json:"tls_mode,omitempty"`
	Status        string     `json:"status"`
	Issuer        string     `json:"issuer,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	DaysRemaining int        `json:"days_remaining,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type MailSection struct {
	Status  string     `json:"status"`
	Message string     `json:"message,omitempty"`
	Records []MXRecord `json:"records,omitempty"`
}

type MXRecord struct {
	Priority  uint16   `json:"priority"`
	Host      string   `json:"host"`
	Addresses []string `json:"addresses,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type SPFResult struct {
	Status     string   `json:"status"`
	Record     string   `json:"record,omitempty"`
	Policy     string   `json:"policy,omitempty"`
	Includes   []string `json:"includes,omitempty"`
	DNSLookups int      `json:"dns_lookups"`
	Message    string   `json:"message,omitempty"`
}

type DKIMResult struct {
	Status         string   `json:"status"`
	Selector       string   `json:"selector,omitempty"`
	SelectorsTried []string `json:"selectors_tried,omitempty"`
	Record         string   `json:"record,omitempty"`
	Message        string   `json:"message,omitempty"`
}

type DMARCResult struct {
	Status  string `json:"status"`
	Record  string `json:"record,omitempty"`
	Policy  string `json:"policy,omitempty"`
	RUA     string `json:"rua,omitempty"`
	Message string `json:"message,omitempty"`
}

type BlacklistResult struct {
	Status             string           `json:"status"`
	Checked            int              `json:"checked"`
	Listed             int              `json:"listed"`
	Timeout            int              `json:"timeout,omitempty"`
	Provider           string           `json:"provider,omitempty"`
	ProviderError      string           `json:"provider_error,omitempty"`
	AdditionalProvider string           `json:"additional_provider,omitempty"`
	AdditionalChecked  int              `json:"additional_checked,omitempty"`
	AdditionalListed   int              `json:"additional_listed,omitempty"`
	AdditionalTimeout  int              `json:"additional_timeout,omitempty"`
	AdditionalError    string           `json:"additional_error,omitempty"`
	AdditionalResults  []BlacklistEntry `json:"additional_results,omitempty"`
	Results            []BlacklistEntry `json:"results,omitempty"`
	Message            string           `json:"message,omitempty"`
}

type BlacklistEntry struct {
	IP                 string `json:"ip"`
	List               string `json:"list"`
	Source             string `json:"source,omitempty"`
	Zone               string `json:"zone,omitempty"`
	Status             string `json:"status"`
	Listed             bool   `json:"listed"`
	Reason             string `json:"reason,omitempty"`
	TTL                uint32 `json:"ttl,omitempty"`
	ResponseTimeMS     int64  `json:"response_time_ms"`
	Error              string `json:"error,omitempty"`
	InfoURL            string `json:"info_url,omitempty"`
	DelistURL          string `json:"delist_url,omitempty"`
	DelistInstructions string `json:"delist_instructions,omitempty"`
}
