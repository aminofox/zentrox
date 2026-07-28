package binding_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aminofox/zentrox/v2/binding"
)

type jsonPayload struct {
	Name string `json:"name"`
}

func TestJSONRejectsMultipleDocuments(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"one"}{"name":"two"}`))
	req.Header.Set("Content-Type", "application/json")

	var dst jsonPayload
	if err := binding.JSON.Bind(req, &dst); err == nil {
		t.Fatal("JSON binder should reject multiple JSON documents")
	}
}

func TestStrictJSONRejectsUnknownFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"one","extra":true}`))
	req.Header.Set("Content-Type", "application/json")

	var dst jsonPayload
	if err := binding.StrictJSON.Bind(req, &dst); err == nil {
		t.Fatal("StrictJSON binder should reject unknown fields")
	}
}

func TestStrictJSONRejectsDuplicateKeys(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"one","name":"two"}`))
	req.Header.Set("Content-Type", "application/json")

	var dst jsonPayload
	if err := binding.StrictJSON.Bind(req, &dst); err == nil {
		t.Fatal("StrictJSON binder should reject duplicate keys")
	}
}

func TestStrictJSONRejectsNestedDuplicateKeys(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"one","items":[{"id":1,"id":2}]}`))
	req.Header.Set("Content-Type", "application/json")

	var dst struct {
		Name  string           `json:"name"`
		Items []map[string]int `json:"items"`
	}
	if err := binding.StrictJSON.Bind(req, &dst); err == nil {
		t.Fatal("StrictJSON binder should reject nested duplicate keys")
	}
}

func TestBindAutoJSONRejectsMultipleDocuments(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"one"} {"name":"two"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	var dst jsonPayload
	if err := binding.Bind(req, &dst); err == nil {
		t.Fatal("auto JSON binding should reject multiple JSON documents")
	}
}
