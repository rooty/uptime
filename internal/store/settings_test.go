package store

import (
	"testing"

	"snippetbook/internal/model"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db
}

func TestEnsureSettingsSeedsDefaultsAndPreservesUpdates(t *testing.T) {
	db := newTestDB(t)
	if err := db.EnsureSettings(); err != nil {
		t.Fatalf("ensure settings: %v", err)
	}

	settings, err := db.Settings()
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	want := model.Settings{CheckTimeout: "10s", CheckHistoryLimit: 500}
	if settings != want {
		t.Fatalf("settings = %+v, want %+v", settings, want)
	}

	updated := model.Settings{CheckTimeout: "2s", CheckHistoryLimit: 25}
	if err := db.UpdateSettings(updated); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if err := db.EnsureSettings(); err != nil {
		t.Fatalf("ensure updated settings: %v", err)
	}
	settings, err = db.Settings()
	if err != nil {
		t.Fatalf("read updated settings: %v", err)
	}
	if settings != updated {
		t.Fatalf("settings after reseed = %+v, want %+v", settings, updated)
	}
}
