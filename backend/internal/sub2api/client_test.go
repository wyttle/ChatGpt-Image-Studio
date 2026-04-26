package sub2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExportOpenAIOAuthAccountsUsesGroupFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/groups/42" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": map[string]any{
					"id": 42,
				},
			})
			return
		}
		if r.URL.Path != "/api/v1/admin/accounts/data" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/v1/admin/accounts/data")
		}
		if got := r.URL.Query().Get("group"); got != "42" {
			t.Fatalf("group query = %q, want %q", got, "42")
		}
		if got := r.URL.Query().Get("platform"); got != "openai" {
			t.Fatalf("platform query = %q, want %q", got, "openai")
		}
		if got := r.URL.Query().Get("type"); got != "oauth" {
			t.Fatalf("type query = %q, want %q", got, "oauth")
		}

		_ = json.NewEncoder(w).Encode(DataPayload{
			Accounts: []DataAccount{
				{
					Name:     "demo",
					Platform: "openai",
					Type:     "oauth",
					Credentials: map[string]any{
						"access_token": "token-1",
						"email":        "demo@example.com",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := New(server.URL, "", "", "api-key", "42", 0, "")
	items, err := client.ExportOpenAIOAuthAccounts(context.Background())
	if err != nil {
		t.Fatalf("ExportOpenAIOAuthAccounts() returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ExportOpenAIOAuthAccounts() len = %d, want 1", len(items))
	}
}

func TestExportOpenAIOAuthAccountsIgnoresMissingGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/groups/123":
			http.Error(w, "group not found", http.StatusNotFound)
		case "/api/v1/admin/accounts/data":
			if got := r.URL.Query().Get("group"); got != "" {
				t.Fatalf("group query = %q, want empty", got)
			}
			_ = json.NewEncoder(w).Encode(DataPayload{
				Accounts: []DataAccount{
					{
						Name:     "demo",
						Platform: "openai",
						Type:     "oauth",
						Credentials: map[string]any{
							"access_token": "token-1",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "", "", "api-key", "123", 0, "")
	items, err := client.ExportOpenAIOAuthAccounts(context.Background())
	if err != nil {
		t.Fatalf("ExportOpenAIOAuthAccounts() returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ExportOpenAIOAuthAccounts() len = %d, want 1", len(items))
	}
}

func TestExportOpenAIOAuthAccountsAcceptsCodeEnvelopeLogin(t *testing.T) {
	loginRequests := 0
	exportRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": map[string]any{
					"access_token": "jwt-token",
					"expires_in":   3600,
				},
			})
		case "/api/v1/admin/accounts/data":
			exportRequests++
			if got := r.Header.Get("Authorization"); got != "Bearer jwt-token" {
				t.Fatalf("Authorization = %q, want Bearer jwt-token", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": DataPayload{
					Accounts: []DataAccount{
						{
							Name:     "demo",
							Platform: "openai",
							Type:     "oauth",
							Credentials: map[string]any{
								"access_token": "token-1",
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "admin@example.com", "password", "", "", 0, "")
	items, err := client.ExportOpenAIOAuthAccounts(context.Background())
	if err != nil {
		t.Fatalf("ExportOpenAIOAuthAccounts() returned error: %v", err)
	}
	if loginRequests != 1 {
		t.Fatalf("login requests = %d, want 1", loginRequests)
	}
	if exportRequests != 1 {
		t.Fatalf("export requests = %d, want 1", exportRequests)
	}
	if len(items) != 1 {
		t.Fatalf("ExportOpenAIOAuthAccounts() len = %d, want 1", len(items))
	}
}

func TestImportOpenAIOAuthAccountsUsesCreateEndpointWhenGroupConfigured(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/groups/99":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": map[string]any{
					"id": 99,
				},
			})
		case "/api/v1/admin/accounts":
			requests++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			groupIDs, ok := body["group_ids"].([]any)
			if !ok || len(groupIDs) != 1 || int(groupIDs[0].(float64)) != 99 {
				t.Fatalf("group_ids = %#v, want [99]", body["group_ids"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "", "", "api-key", "99", 0, "")
	result, err := client.ImportOpenAIOAuthAccounts(context.Background(), []RemoteAccount{
		{Name: "demo", Platform: "openai", Type: "oauth", Credentials: map[string]any{"access_token": "token-1"}},
	})
	if err != nil {
		t.Fatalf("ImportOpenAIOAuthAccounts() returned error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("create account requests = %d, want 1", requests)
	}
	if result.AccountCreated != 1 {
		t.Fatalf("AccountCreated = %d, want 1", result.AccountCreated)
	}
}

func TestImportOpenAIOAuthAccountsFallsBackWhenGroupMissing(t *testing.T) {
	groupRequests := 0
	importRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/groups/123":
			groupRequests++
			http.Error(w, "group not found", http.StatusNotFound)
		case "/api/v1/admin/accounts/data":
			importRequests++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if body["skip_default_group_bind"] != true {
				t.Fatalf("skip_default_group_bind = %#v, want true", body["skip_default_group_bind"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": map[string]any{
					"account_created": 1,
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "", "", "api-key", "123", 0, "")
	result, err := client.ImportOpenAIOAuthAccounts(context.Background(), []RemoteAccount{
		{Name: "demo", Platform: "openai", Type: "oauth", Credentials: map[string]any{"access_token": "token-1"}},
	})
	if err != nil {
		t.Fatalf("ImportOpenAIOAuthAccounts() returned error: %v", err)
	}
	if groupRequests != 1 {
		t.Fatalf("group requests = %d, want 1", groupRequests)
	}
	if importRequests != 1 {
		t.Fatalf("import requests = %d, want 1", importRequests)
	}
	if result.AccountCreated != 1 {
		t.Fatalf("AccountCreated = %d, want 1", result.AccountCreated)
	}
}

func TestListGroupsUsesAuthenticatedAdminAPI(t *testing.T) {
	loginRequests := 0
	groupRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": map[string]any{
					"access_token": "jwt-token",
					"expires_in":   3600,
				},
			})
		case "/api/v1/admin/groups/all":
			groupRequests++
			if got := r.Header.Get("Authorization"); got != "Bearer jwt-token" {
				t.Fatalf("Authorization = %q, want Bearer jwt-token", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": []map[string]any{
					{
						"id":          12,
						"name":        "openai-default",
						"description": "demo",
						"platform":    "openai",
						"status":      "active",
					},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "admin@example.com", "password", "", "", 0, "")
	groups, err := client.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups() returned error: %v", err)
	}
	if loginRequests != 1 || groupRequests != 1 {
		t.Fatalf("loginRequests=%d groupRequests=%d, want 1 and 1", loginRequests, groupRequests)
	}
	if len(groups) != 1 || groups[0].ID != 12 || groups[0].Name != "openai-default" {
		t.Fatalf("unexpected groups: %#v", groups)
	}
}
