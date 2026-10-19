package domaincheck

import (
	"testing"
	"time"

	"snippetbook/internal/model"
)

func TestCompareRegistrationListsChangedFields(t *testing.T) {
	previousExpiry := time.Date(2027, time.March, 18, 12, 30, 3, 0, time.UTC)
	currentExpiry := previousExpiry.Add(24 * time.Hour)
	previous := model.DomainRegistration{
		Status:      "ok",
		Registrar:   "Old Registrar",
		ExpiresAt:   &previousExpiry,
		Fingerprint: "old",
	}
	current := CompareRegistration(previous, model.DomainRegistration{
		Status:      "ok",
		Registrar:   "New Registrar",
		ExpiresAt:   &currentExpiry,
		Fingerprint: "new",
	})

	if !current.Changed || len(current.Changes) != 2 {
		t.Fatalf("expected two registration changes, got changed=%v changes=%+v", current.Changed, current.Changes)
	}
	if current.Changes[0].Field != "Регистратор" || current.Changes[1].Field != "Окончание регистрации" {
		t.Fatalf("unexpected changes: %+v", current.Changes)
	}
}

func TestCompareRegistrationIgnoresLegacyEmptySnapshot(t *testing.T) {
	current := CompareRegistration(model.DomainRegistration{}, model.DomainRegistration{Status: "ok"})
	if current.Changed || len(current.Changes) != 0 {
		t.Fatalf("legacy empty snapshot must not be reported as a change: %+v", current)
	}
}

func TestCompareRegistrationNormalizesLegacyRegistrar(t *testing.T) {
	current := CompareRegistration(
		model.DomainRegistration{Status: "ok", Registrar: `"fn",{},"text","Old Registrar"], ["org",{},"text","Old Registrar"]`, Fingerprint: "old"},
		model.DomainRegistration{Status: "ok", Registrar: "New Registrar", Fingerprint: "new"},
	)
	if len(current.Changes) != 1 || current.Changes[0].Previous != "Old Registrar" {
		t.Fatalf("legacy registrar was not normalized: %+v", current.Changes)
	}
}
