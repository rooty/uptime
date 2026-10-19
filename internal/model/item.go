package model

import "time"

// Kind values for items.
const (
	KindSnippet = "snippet"
	KindNote    = "note"
	KindSecret  = "secret"
)

// Item is a single entry in the notebook.
type Item struct {
	ID           int64      `json:"id"`
	UUID         string     `json:"uuid"`
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	Language     string     `json:"language,omitempty"`
	Content      string     `json:"content,omitempty"`
	RenderedHTML string     `json:"rendered_html,omitempty"`
	Tags         []string   `json:"tags"`
	Favorite     bool       `json:"favorite"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

// SecretPayload is the decrypted value of a secret item returned on demand.
type SecretPayload struct {
	ItemID int64  `json:"item_id"`
	Value  string `json:"value"`
}
