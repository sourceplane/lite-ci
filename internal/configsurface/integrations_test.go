package configsurface

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestConnectIntegration_TokenPaste(t *testing.T) {
	var gotPath, gotToken, gotAuth string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotToken = body["parentToken"]
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"data":{"connection":{"id":"int_abc","provider":"supabase","status":"active"}},"meta":{"requestId":"r1"}}`))
	})
	conn, err := client.ConnectIntegration(context.Background(), "ws_1", "supabase", "sbp_secret123", "My Org")
	if err != nil {
		t.Fatalf("ConnectIntegration: %v", err)
	}
	if gotPath != "/v1/organizations/ws_1/integrations/supabase/connect" {
		t.Errorf("path = %q", gotPath)
	}
	if gotToken != "sbp_secret123" {
		t.Errorf("parentToken not sent verbatim: %q", gotToken)
	}
	if gotAuth == "" {
		t.Errorf("no bearer auth sent")
	}
	if conn.ID != "int_abc" || conn.Status != "active" {
		t.Errorf("connection = %+v", conn)
	}
}

func TestConnectIntegration_EmptyTokenRefusedLocally(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request must be sent for an empty token")
	})
	if _, err := client.ConnectIntegration(context.Background(), "ws_1", "cloudflare", "   ", ""); err == nil {
		t.Fatal("expected error for empty token")
	}
}
