package model

import "time"

// IPGeolocation is shared by every service that resolves to the same IP.
type IPGeolocation struct {
	IP            string     `json:"ip"`
	Country       string     `json:"country"`
	CountryCode   string     `json:"countryCode"`
	Region        string     `json:"regionName"`
	City          string     `json:"city"`
	Latitude      *float64   `json:"lat"`
	Longitude     *float64   `json:"lon"`
	Timezone      string     `json:"timezone"`
	ISP           string     `json:"isp"`
	Organization  string     `json:"org"`
	AS            string     `json:"as"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastAttemptAt time.Time  `json:"last_attempt_at"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	Failures      int        `json:"failures"`
	Status        string     `json:"status"`
	Error         string     `json:"error,omitempty"`
}

type ServiceLocation struct {
	ServiceID     int64     `json:"service_id"`
	Hostname      string    `json:"hostname"`
	IPs           []string  `json:"ips"`
	ResolvedAt    time.Time `json:"resolved_at"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	Error         string    `json:"error,omitempty"`
}
