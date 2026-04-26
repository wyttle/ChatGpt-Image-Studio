package newapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestListCodexChannelsPaginatesWithNewAPIMaxPageSize(t *testing.T) {
	requestedPages := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" {
			t.Fatalf("path = %q, want /api/channel/", r.URL.Path)
		}
		if got := r.URL.Query().Get("page_size"); got != "100" {
			t.Fatalf("page_size = %q, want 100", got)
		}
		page, err := strconv.Atoi(r.URL.Query().Get("p"))
		if err != nil {
			t.Fatalf("invalid page: %v", err)
		}
		requestedPages = append(requestedPages, page)

		count := 100
		if page == 3 {
			count = 32
		}
		items := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			items = append(items, map[string]any{
				"id":         int64((page-1)*100 + i + 1),
				"name":       "demo",
				"remark":     "",
				"other_info": "{}",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"items": items,
				"total": 232,
			},
		})
	}))
	defer server.Close()

	client := New(server.URL, "", "", "token", 1, "", 0, "")
	channels, err := client.ListCodexChannels(context.Background())
	if err != nil {
		t.Fatalf("ListCodexChannels() returned error: %v", err)
	}
	if len(channels) != 232 {
		t.Fatalf("ListCodexChannels() len = %d, want 232", len(channels))
	}
	if got := len(requestedPages); got != 3 {
		t.Fatalf("requested page count = %d, want 3", got)
	}
}

func TestGetSelfUsesLoginFlow(t *testing.T) {
	loginRequests := 0
	selfRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			loginRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"id": 7,
				},
			})
		case "/api/user/self":
			selfRequests++
			if got := r.Header.Get("New-API-User"); got != "7" {
				t.Fatalf("New-API-User = %q, want 7", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"id":           7,
					"username":     "demo",
					"display_name": "Demo",
					"email":        "demo@example.com",
					"role":         100,
					"status":       1,
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "demo", "password", "", 0, "", 0, "")
	user, err := client.GetSelf(context.Background())
	if err != nil {
		t.Fatalf("GetSelf() returned error: %v", err)
	}
	if loginRequests != 1 || selfRequests != 1 {
		t.Fatalf("loginRequests=%d selfRequests=%d, want 1 and 1", loginRequests, selfRequests)
	}
	if user.ID != 7 || user.Username != "demo" {
		t.Fatalf("unexpected user: %#v", user)
	}
}

func TestGenerateAccessTokenReturnsTokenAndUserID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/token":
			if got := r.Header.Get("Authorization"); got != "existing-token" {
				t.Fatalf("Authorization = %q, want existing-token", got)
			}
			if got := r.Header.Get("New-API-User"); got != "9" {
				t.Fatalf("New-API-User = %q, want 9", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    "new-token-123",
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "", "", "existing-token", 9, "", 0, "")
	token, userID, err := client.GenerateAccessToken(context.Background())
	if err != nil {
		t.Fatalf("GenerateAccessToken() returned error: %v", err)
	}
	if token != "new-token-123" || userID != 9 {
		t.Fatalf("token=%q userID=%d", token, userID)
	}
}
