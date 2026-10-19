package domaincheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientRetriesRDAPLookup(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Accept"); got == "" {
			t.Error("Accept header is empty")
		}
		if requests.Add(1) == 1 {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "application/rdap+json")
		_, _ = fmt.Fprint(writer, `{"ldhName":"example.com","events":[{"eventAction":"expiration","eventDate":"2027-03-18T12:30:00Z"}],"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Example Registrar"]]]}]}`)
	}))
	defer server.Close()

	client := Client{HTTP: server.Client(), Endpoints: []string{server.URL + "/domain/"}}
	result, err := client.Check(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || result.Registrar != "Example Registrar" || result.ExpiresAt == nil {
		t.Fatalf("unexpected registration result: %+v", result)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestRDAPEndpointsUseGenericEndpointForExampleDomain(t *testing.T) {
	endpoints := rdapEndpoints("example.com")
	if len(endpoints) != 1 || endpoints[0] != "https://rdap.org/domain/" {
		t.Fatalf("endpoints = %v", endpoints)
	}
}
