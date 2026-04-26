package sub2api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/outboundproxy"
)

type Client struct {
	baseURL      string
	email        string
	password     string
	apiKey       string
	groupID      string
	httpClient   *http.Client
	tokenMu      sync.Mutex
	cachedToken  string
	tokenExpires time.Time
}

type RemoteAccount struct {
	ID          string
	Name        string
	Platform    string
	Type        string
	Credentials map[string]any
}

type Group struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Platform    string `json:"platform"`
	Status      string `json:"status"`
}

type DataPayload struct {
	Type       string        `json:"type,omitempty"`
	Version    int           `json:"version,omitempty"`
	ExportedAt string        `json:"exported_at,omitempty"`
	Proxies    []interface{} `json:"proxies"`
	Accounts   []DataAccount `json:"accounts"`
}

type DataAccount struct {
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"credentials"`
}

type ImportResult struct {
	ProxyCreated   int           `json:"proxy_created"`
	ProxyReused    int           `json:"proxy_reused"`
	ProxyFailed    int           `json:"proxy_failed"`
	AccountCreated int           `json:"account_created"`
	AccountFailed  int           `json:"account_failed"`
	Errors         []ImportError `json:"errors,omitempty"`
}

type ImportError struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	ProxyKey string `json:"proxy_key,omitempty"`
	Message  string `json:"message"`
}

func New(baseURL, email, password, apiKey, groupID string, timeout time.Duration, proxyURL string) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	transport, err := outboundproxy.NewHTTPTransport(proxyURL)
	if err != nil {
		panic(err)
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		email:      strings.TrimSpace(email),
		password:   strings.TrimSpace(password),
		apiKey:     strings.TrimSpace(apiKey),
		groupID:    strings.TrimSpace(groupID),
		httpClient: &http.Client{Timeout: timeout, Transport: transport},
	}
}

func (c *Client) Configured() bool {
	if c == nil || c.baseURL == "" {
		return false
	}
	if c.apiKey != "" {
		return true
	}
	return c.email != "" && c.password != ""
}

func (c *Client) ExportOpenAIOAuthAccounts(ctx context.Context) ([]RemoteAccount, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("sub2api is not configured")
	}

	query := url.Values{}
	query.Set("include_proxies", "false")
	query.Set("platform", "openai")
	query.Set("type", "oauth")
	if groupID, ok := c.parsedGroupID(); ok {
		exists, err := c.groupExists(ctx, groupID)
		if err == nil && exists {
			query.Set("group", c.groupID)
		}
	}

	payload := DataPayload{}
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/admin/accounts/data?"+query.Encode(), nil, &payload); err != nil {
		return nil, err
	}

	items := make([]RemoteAccount, 0, len(payload.Accounts))
	for _, account := range payload.Accounts {
		if !strings.EqualFold(strings.TrimSpace(account.Platform), "openai") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(account.Type), "oauth") {
			continue
		}
		credentials := cloneMap(account.Credentials)
		accessToken := strings.TrimSpace(stringValue(credentials["access_token"]))
		if accessToken == "" {
			continue
		}
		items = append(items, RemoteAccount{
			ID:          strings.TrimSpace(firstNonEmpty(stringValue(credentials["account_id"]), stringValue(credentials["chatgpt_account_id"]), stringValue(credentials["user_id"]))),
			Name:        strings.TrimSpace(account.Name),
			Platform:    "openai",
			Type:        "oauth",
			Credentials: credentials,
		})
	}
	return items, nil
}

func (c *Client) ListGroups(ctx context.Context) ([]Group, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("sub2api is not configured")
	}

	items := make([]Group, 0)
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/admin/groups/all", nil, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Client) ImportOpenAIOAuthAccounts(ctx context.Context, accounts []RemoteAccount) (*ImportResult, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("sub2api is not configured")
	}

	groupID, hasGroupID := c.parsedGroupID()
	if hasGroupID {
		exists, err := c.groupExists(ctx, groupID)
		if err == nil && exists {
			return c.createAccountsWithGroup(ctx, accounts, groupID)
		}
	}

	return c.importOpenAIOAuthAccountsData(ctx, accounts)
}

func (c *Client) importOpenAIOAuthAccountsData(ctx context.Context, accounts []RemoteAccount) (*ImportResult, error) {
	payload := DataPayload{
		Type:       "sub2api-data",
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    []interface{}{},
		Accounts:   make([]DataAccount, 0, len(accounts)),
	}
	for _, account := range accounts {
		payload.Accounts = append(payload.Accounts, DataAccount{
			Name:        strings.TrimSpace(account.Name),
			Platform:    "openai",
			Type:        "oauth",
			Credentials: cloneMap(account.Credentials),
		})
	}

	body := map[string]any{
		"data":                    payload,
		"skip_default_group_bind": true,
	}
	result := &ImportResult{}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/admin/accounts/data", body, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) groupExists(ctx context.Context, groupID int64) (bool, error) {
	var response struct {
		ID int64 `json:"id"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/admin/groups/"+strconv.FormatInt(groupID, 10), nil, &response)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") || strings.Contains(strings.ToLower(err.Error()), "not found") {
			return false, nil
		}
		return false, err
	}
	return response.ID == groupID, nil
}

func (c *Client) createAccountsWithGroup(ctx context.Context, accounts []RemoteAccount, groupID int64) (*ImportResult, error) {
	result := &ImportResult{}
	for _, account := range accounts {
		body := map[string]any{
			"name":        strings.TrimSpace(account.Name),
			"platform":    "openai",
			"type":        "oauth",
			"credentials": cloneMap(account.Credentials),
			"concurrency": 3,
			"priority":    50,
			"group_ids":   []int64{groupID},
		}

		if err := c.doJSON(ctx, http.MethodPost, "/api/v1/admin/accounts", body, nil); err != nil {
			result.AccountFailed++
			continue
		}
		result.AccountCreated++
	}
	return result, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody any, out any) error {
	body, err := c.newBody(requestBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if err := c.applyAuth(ctx, req); err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("sub2api request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var envelope struct {
		Success *bool           `json:"success"`
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Data) > 0 {
		if envelope.Code != nil && *envelope.Code != 0 {
			return fmt.Errorf("sub2api request failed: %s", firstNonEmpty(envelope.Message, fmt.Sprintf("code %d", *envelope.Code)))
		}
		if envelope.Success != nil && !*envelope.Success {
			return fmt.Errorf("sub2api request failed: %s", firstNonEmpty(envelope.Message, "success is false"))
		}
		return json.Unmarshal(envelope.Data, out)
	}

	return json.Unmarshal(raw, out)
}

func (c *Client) applyAuth(ctx context.Context, req *http.Request) error {
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
		return nil
	}

	token, err := c.getToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func (c *Client) getToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.cachedToken != "" && time.Now().Before(c.tokenExpires) {
		return c.cachedToken, nil
	}

	payload := map[string]string{
		"email":    c.email,
		"password": c.password,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("sub2api login failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var envelope struct {
		Success *bool  `json:"success"`
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Data    struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", err
	}
	if envelope.Code != nil && *envelope.Code != 0 {
		return "", fmt.Errorf("sub2api login failed: %s", firstNonEmpty(envelope.Message, fmt.Sprintf("code %d", *envelope.Code)))
	}
	if envelope.Success != nil && !*envelope.Success {
		return "", fmt.Errorf("sub2api login failed: %s", firstNonEmpty(envelope.Message, "success is false"))
	}
	token := strings.TrimSpace(envelope.Data.AccessToken)
	if token == "" {
		return "", fmt.Errorf("sub2api login failed: access_token is empty")
	}
	expiresIn := envelope.Data.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	c.cachedToken = token
	c.tokenExpires = time.Now().Add(time.Duration(expiresIn-300) * time.Second)
	return token, nil
}

func (c *Client) newBody(value any) (io.Reader, error) {
	if value == nil {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(payload), nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case uint32:
		return strconv.FormatUint(uint64(typed), 10)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (c *Client) parsedGroupID() (int64, bool) {
	trimmed := strings.TrimSpace(c.groupID)
	if trimmed == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}
