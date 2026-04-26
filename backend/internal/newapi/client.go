package newapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/outboundproxy"
)

const codexChannelType = 57
const syncMetadataKey = "chatgpt_image_studio_sync"

type Client struct {
	baseURL       string
	username      string
	password      string
	accessToken   string
	userID        int
	sessionCookie string
	httpClient    *http.Client
	mu            sync.Mutex
}

type Channel struct {
	ID        int64
	Name      string
	Remark    string
	OtherInfo map[string]any
}

type User struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        int    `json:"role"`
	Status      int    `json:"status"`
}

type SyncMetadata struct {
	IdentityKey string         `json:"identity_key"`
	AccountType string         `json:"account_type"`
	AuthData    map[string]any `json:"auth_data"`
}

func New(baseURL, username, password, accessToken string, userID int, sessionCookie string, timeout time.Duration, proxyURL string) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	transport, err := outboundproxy.NewHTTPTransport(proxyURL)
	if err != nil {
		panic(err)
	}
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL:       strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		username:      strings.TrimSpace(username),
		password:      strings.TrimSpace(password),
		accessToken:   strings.TrimSpace(accessToken),
		userID:        userID,
		sessionCookie: strings.TrimSpace(sessionCookie),
		httpClient:    &http.Client{Timeout: timeout, Transport: transport, Jar: jar},
	}
}

func (c *Client) Configured() bool {
	if c == nil || c.baseURL == "" {
		return false
	}
	if c.userID > 0 && (c.accessToken != "" || c.sessionCookie != "") {
		return true
	}
	return c.username != "" && c.password != ""
}

func (c *Client) ListCodexChannels(ctx context.Context) ([]Channel, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("newapi is not configured")
	}

	const pageSize = 100
	items := make([]Channel, 0)
	page := 1
	for {
		path := fmt.Sprintf("/api/channel/?p=%d&page_size=%d&type=%d", page, pageSize, codexChannelType)
		var payload struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Data    struct {
				Items []struct {
					ID        int64  `json:"id"`
					Name      string `json:"name"`
					Remark    string `json:"remark"`
					OtherInfo string `json:"other_info"`
				} `json:"items"`
				Total int `json:"total"`
			} `json:"data"`
		}
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload); err != nil {
			return nil, err
		}
		if !payload.Success && strings.TrimSpace(payload.Message) != "" {
			return nil, fmt.Errorf("newapi list channels failed: %s", strings.TrimSpace(payload.Message))
		}
		if len(payload.Data.Items) == 0 {
			break
		}
		for _, item := range payload.Data.Items {
			items = append(items, Channel{
				ID:        item.ID,
				Name:      strings.TrimSpace(item.Name),
				Remark:    strings.TrimSpace(item.Remark),
				OtherInfo: parseJSONObject(item.OtherInfo),
			})
		}
		if len(items) >= payload.Data.Total || len(payload.Data.Items) < pageSize {
			break
		}
		page++
	}
	return items, nil
}

func (c *Client) CreateCodexChannel(ctx context.Context, name, identityKey, accountType string, authData map[string]any) error {
	if !c.Configured() {
		return fmt.Errorf("newapi is not configured")
	}

	keyPayload, err := json.Marshal(buildCodexKeyPayload(authData))
	if err != nil {
		return err
	}
	otherInfoBytes, err := json.Marshal(map[string]any{
		syncMetadataKey: SyncMetadata{
			IdentityKey: strings.TrimSpace(identityKey),
			AccountType: strings.TrimSpace(accountType),
			AuthData:    cloneMap(authData),
		},
	})
	if err != nil {
		return err
	}

	body := map[string]any{
		"mode": "single",
		"channel": map[string]any{
			"type":       codexChannelType,
			"name":       strings.TrimSpace(name),
			"status":     1,
			"models":     "gpt-image-2,gpt-5.4-mini,gpt-5.4,gpt-5.5,gpt-5-5-thinking,auto",
			"group":      "default",
			"priority":   0,
			"key":        string(keyPayload),
			"remark":     "chatgpt-image-studio::" + strings.TrimSpace(identityKey),
			"other_info": string(otherInfoBytes),
		},
	}

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/channel/", body, &result); err != nil {
		return err
	}
	if !result.Success && strings.TrimSpace(result.Message) != "" {
		return fmt.Errorf("newapi create channel failed: %s", strings.TrimSpace(result.Message))
	}
	return nil
}

func (c *Client) TryReadCodexKey(ctx context.Context, channelID int64) (map[string]any, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("newapi is not configured")
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/channel/"+strconv.FormatInt(channelID, 10)+"/key", map[string]any{}, &payload); err != nil {
		return nil, err
	}
	if !payload.Success {
		return nil, fmt.Errorf("newapi get channel key failed: %s", strings.TrimSpace(payload.Message))
	}
	return parseJSONObject(payload.Data.Key), nil
}

func (c *Client) GetSelf(ctx context.Context) (*User, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("newapi is not configured")
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    User   `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/user/self", nil, &payload); err != nil {
		return nil, err
	}
	if !payload.Success && strings.TrimSpace(payload.Message) != "" {
		return nil, fmt.Errorf("newapi get self failed: %s", strings.TrimSpace(payload.Message))
	}
	if payload.Data.ID <= 0 {
		return nil, fmt.Errorf("newapi get self failed: missing user id")
	}
	return &payload.Data, nil
}

func (c *Client) GenerateAccessToken(ctx context.Context) (string, int, error) {
	if !c.Configured() {
		return "", 0, fmt.Errorf("newapi is not configured")
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/user/token", nil, &payload); err != nil {
		return "", 0, err
	}
	if !payload.Success && strings.TrimSpace(payload.Message) != "" {
		return "", 0, fmt.Errorf("newapi generate access token failed: %s", strings.TrimSpace(payload.Message))
	}
	token := strings.TrimSpace(payload.Data)
	if token == "" {
		return "", 0, fmt.Errorf("newapi generate access token failed: token is empty")
	}
	if c.userID <= 0 {
		return "", 0, fmt.Errorf("newapi generate access token failed: missing user id")
	}
	return token, c.userID, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody any, out any) error {
	if err := c.ensureSession(ctx); err != nil {
		return err
	}

	body, err := c.newBody(requestBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userID > 0 {
		req.Header.Set("New-API-User", strconv.Itoa(c.userID))
	}
	if c.accessToken != "" {
		req.Header.Set("Authorization", c.accessToken)
	}
	if c.sessionCookie != "" {
		req.Header.Set("Cookie", c.sessionCookie)
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
		return fmt.Errorf("newapi request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) ensureSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.userID > 0 && (c.accessToken != "" || c.sessionCookie != "") {
		return nil
	}
	if c.username == "" || c.password == "" {
		return fmt.Errorf("newapi requires access_token+user_id, session_cookie+user_id, or username+password")
	}

	body, err := json.Marshal(map[string]string{
		"username": c.username,
		"password": c.password,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/user/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("newapi login failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	if !payload.Success {
		return fmt.Errorf("newapi login failed: %s", strings.TrimSpace(payload.Message))
	}
	if payload.Data.ID <= 0 {
		return fmt.Errorf("newapi login failed: missing user id")
	}
	c.userID = payload.Data.ID
	return nil
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

func buildCodexKeyPayload(authData map[string]any) map[string]any {
	key := cloneMap(authData)
	if strings.TrimSpace(stringValue(key["access_token"])) != "" && strings.TrimSpace(stringValue(key["account_id"])) == "" {
		key["account_id"] = firstNonEmpty(
			stringValue(key["chatgpt_account_id"]),
			stringValue(key["user_id"]),
			stringValue(key["sub"]),
		)
	}
	return key
}

func MetadataFromChannel(channel Channel) (*SyncMetadata, bool) {
	raw, ok := channel.OtherInfo[syncMetadataKey]
	if !ok || raw == nil {
		return nil, false
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var metadata SyncMetadata
	if err := json.Unmarshal(bytes, &metadata); err != nil {
		return nil, false
	}
	if strings.TrimSpace(metadata.IdentityKey) == "" {
		return nil, false
	}
	return &metadata, true
}

func parseJSONObject(raw string) map[string]any {
	result := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
		return map[string]any{}
	}
	return result
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
