package productclassification_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/productclassification"
)

// fakeDoer is a stub productclassification.HTTPDoer so these tests never
// hit the network.
type fakeDoer struct {
	resp *http.Response
	err  error
	req  *http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClient_GetClassification_200_Hazmat(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusOK, `{"sku":"sku-1","handlingTags":["Hazmat"],"temperatureClass":""}`)} //nolint:bodyclose
	client := productclassification.NewClient("http://inventory-storage.local", doer)

	view, err := client.GetClassification(context.Background(), "sku-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !view.Known {
		t.Fatalf("expected Known=true")
	}
	if !view.HasTag("Hazmat") {
		t.Fatalf("expected Hazmat tag, got %v", view.HandlingTags)
	}
	if doer.req.URL.Path != "/products/sku-1/classification" {
		t.Fatalf("expected path /products/sku-1/classification, got %s", doer.req.URL.Path)
	}
}

func TestClient_GetClassification_200_Fragile(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusOK, `{"sku":"sku-2","handlingTags":["Fragile"],"temperatureClass":""}`)} //nolint:bodyclose
	client := productclassification.NewClient("http://inventory-storage.local", doer)

	view, err := client.GetClassification(context.Background(), "sku-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !view.HasTag("Fragile") {
		t.Fatalf("expected Fragile tag, got %v", view.HandlingTags)
	}
	if view.HasTag("Hazmat") {
		t.Fatalf("did not expect Hazmat tag, got %v", view.HandlingTags)
	}
}

func TestClient_GetClassification_404_FailsOpen(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusNotFound, "")} //nolint:bodyclose
	client := productclassification.NewClient("http://inventory-storage.local", doer)

	view, err := client.GetClassification(context.Background(), "sku-unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.Known {
		t.Fatalf("expected Known=false on 404")
	}
}

func TestClient_GetClassification_500_ReturnsError(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusInternalServerError, "")} //nolint:bodyclose
	client := productclassification.NewClient("http://inventory-storage.local", doer)

	_, err := client.GetClassification(context.Background(), "sku-1")
	if !errors.Is(err, productclassification.ErrUnexpectedStatus) {
		t.Fatalf("expected ErrUnexpectedStatus, got %v", err)
	}
}

func TestClient_GetClassification_TransportError_Propagates(t *testing.T) {
	transportErr := errors.New("connection refused")
	doer := &fakeDoer{err: transportErr}
	client := productclassification.NewClient("http://inventory-storage.local", doer)

	_, err := client.GetClassification(context.Background(), "sku-1")
	if !errors.Is(err, transportErr) {
		t.Fatalf("expected transport error to propagate, got %v", err)
	}
}

func TestClient_GetClassification_MalformedJSON_ReturnsError(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusOK, `{not-json`)} //nolint:bodyclose
	client := productclassification.NewClient("http://inventory-storage.local", doer)

	_, err := client.GetClassification(context.Background(), "sku-1")
	if err == nil {
		t.Fatalf("expected an error decoding malformed JSON")
	}
}

func TestNewClient_NilDoer_DefaultsToRealHTTPClient(t *testing.T) {
	client := productclassification.NewClient("http://inventory-storage.local", nil)
	if client == nil {
		t.Fatalf("expected a non-nil client")
	}
}

func TestPermissiveLookup_AlwaysReportsUnknown(t *testing.T) {
	lookup := productclassification.NewPermissiveLookup()
	view, err := lookup.GetClassification(context.Background(), "sku-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.Known {
		t.Fatalf("expected Known=false from PermissiveLookup")
	}
}

// sanity: confirm we serialize the request the way inventory-storage
// expects (Accept header set, GET method).
func TestClient_GetClassification_SetsAcceptHeaderAndMethod(t *testing.T) {
	doer := &fakeDoer{resp: jsonResponse(http.StatusOK, `{"sku":"sku-1","handlingTags":[],"temperatureClass":""}`)} //nolint:bodyclose
	client := productclassification.NewClient("http://inventory-storage.local", doer)

	_, err := client.GetClassification(context.Background(), "sku-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.req.Method != http.MethodGet {
		t.Fatalf("expected GET, got %s", doer.req.Method)
	}
	if doer.req.Header.Get("Accept") != "application/json" {
		t.Fatalf("expected Accept: application/json header")
	}
}
