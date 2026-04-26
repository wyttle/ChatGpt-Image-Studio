package api

import (
	"testing"

	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/newapi"
)

func TestIdentityKeyFromNewAPIRemark(t *testing.T) {
	got := identityKeyFromNewAPIRemark("chatgpt-image-studio::codex::demo@example.com")
	if got != "codex::demo@example.com" {
		t.Fatalf("identityKeyFromNewAPIRemark() = %q", got)
	}
}

func TestResolveNewAPIIdentityKeyUsesRemarkFallback(t *testing.T) {
	info := describeNewAPIRemoteChannel(newapi.Channel{
		Name:   "demo@example.com",
		Remark: "chatgpt-image-studio::codex::demo@example.com",
	})
	if info.pullable {
		t.Fatal("remark fallback should not be treated as pullable auth data")
	}
	if info.identityKey != "codex::demo@example.com" {
		t.Fatalf("describeNewAPIRemoteChannel() identityKey = %q", info.identityKey)
	}
	if info.normalizedName != "demo@example.com" {
		t.Fatalf("describeNewAPIRemoteChannel() normalizedName = %q", info.normalizedName)
	}
}

func TestMatchesNewAPIRemoteChannelFallsBackToName(t *testing.T) {
	auth := accounts.LocalAuth{
		Name:  "demo.json",
		Email: "demo@example.com",
		Data: map[string]any{
			"type":         "codex",
			"access_token": "token-1",
			"email":        "demo@example.com",
		},
	}

	if !matchesNewAPIRemoteChannel(map[string]struct{}{}, map[string]struct{}{"demo@example.com": {}}, auth) {
		t.Fatal("matchesNewAPIRemoteChannel() should match by normalized remote account name")
	}
}

func TestDescribeNewAPIRemoteChannelWithoutMetadataIsNotPullable(t *testing.T) {
	info := describeNewAPIRemoteChannel(newapi.Channel{Name: "legacy@example.com"})
	if info.pullable {
		t.Fatal("legacy channel without embedded auth data should not be pullable")
	}
	if info.normalizedName != "legacy@example.com" {
		t.Fatalf("normalizedName = %q, want %q", info.normalizedName, "legacy@example.com")
	}
}

func TestFilterSyncableSourceAccountsSkipsTokenAccounts(t *testing.T) {
	filtered := filterSyncableSourceAccounts([]accounts.LocalAuth{
		{Name: "token.json", SourceKind: accounts.AccountSourceKindToken},
		{Name: "auth.json", SourceKind: accounts.AccountSourceKindAuthFile},
		{Name: "legacy.json"},
	})

	if len(filtered) != 2 {
		t.Fatalf("filtered count = %d, want 2", len(filtered))
	}
	if filtered[0].Name != "auth.json" || filtered[1].Name != "legacy.json" {
		t.Fatalf("filtered accounts = %#v", filtered)
	}
}
