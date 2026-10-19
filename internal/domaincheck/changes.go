package domaincheck

import (
	"regexp"
	"strings"
	"time"

	"snippetbook/internal/model"
)

var legacyRegistrarText = regexp.MustCompile(`"text","([^"]+)"`)

// CompareRegistration returns the current RDAP result with a human-readable
// list of fields that differ from the previous check.
func CompareRegistration(previous, current model.DomainRegistration) model.DomainRegistration {
	if previous.Status == "" && previous.Registrar == "" && previous.ExpiresAt == nil && previous.Fingerprint == "" {
		current.Changes = nil
		current.Changed = false
		return current
	}
	changes := make([]model.RegistrationChange, 0)
	addChange := func(field, oldValue, newValue string) {
		if oldValue != newValue {
			changes = append(changes, model.RegistrationChange{Field: field, Previous: oldValue, Current: newValue})
		}
	}

	addChange("Статус", previous.Status, current.Status)
	addChange("Регистратор", normalizeRegistrar(previous.Registrar), normalizeRegistrar(current.Registrar))
	addChange("Окончание регистрации", registrationDate(previous.ExpiresAt), registrationDate(current.ExpiresAt))
	if previous.Fingerprint != "" && current.Fingerprint != "" && previous.Fingerprint != current.Fingerprint && len(changes) == 0 {
		changes = append(changes, model.RegistrationChange{
			Field:    "Прочие данные RDAP",
			Previous: "предыдущий снимок",
			Current:  "новый снимок",
		})
	}
	current.Changes = changes
	current.Changed = len(changes) > 0
	return current
}

func normalizeRegistrar(value string) string {
	value = strings.TrimSpace(value)
	if match := legacyRegistrarText.FindStringSubmatch(value); len(match) == 2 {
		return match[1]
	}
	return value
}

func registrationDate(value *time.Time) string {
	if value == nil {
		return "не определено"
	}
	return value.UTC().Format(time.RFC3339)
}
