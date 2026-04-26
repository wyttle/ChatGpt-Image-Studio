package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"chatgpt2api/internal/newapi"
	"chatgpt2api/internal/outboundproxy"
	"chatgpt2api/internal/sub2api"
)

type integrationTestRequest struct {
	Source  string `json:"source"`
	CPA     integrationCPAConfig
	NewAPI  integrationNewAPIConfig
	Sub2API integrationSub2APIConfig
}

type integrationCPAConfig struct {
	BaseURL        string `json:"baseUrl"`
	APIKey         string `json:"apiKey"`
	RequestTimeout int    `json:"requestTimeout"`
}

type integrationNewAPIConfig struct {
	BaseURL        string `json:"baseUrl"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	AccessToken    string `json:"accessToken"`
	UserID         int    `json:"userId"`
	SessionCookie  string `json:"sessionCookie"`
	RequestTimeout int    `json:"requestTimeout"`
}

type integrationSub2APIConfig struct {
	BaseURL        string `json:"baseUrl"`
	Email          string `json:"email"`
	Password       string `json:"password"`
	APIKey         string `json:"apiKey"`
	GroupID        string `json:"groupId"`
	RequestTimeout int    `json:"requestTimeout"`
}

type integrationTestResponse struct {
	OK         bool   `json:"ok"`
	Source     string `json:"source"`
	Message    string `json:"message"`
	Status     int    `json:"status"`
	Latency    int64  `json:"latency"`
	UserID     int    `json:"userId,omitempty"`
	Username   string `json:"username,omitempty"`
	Email      string `json:"email,omitempty"`
	GroupCount int    `json:"groupCount,omitempty"`
}

type newAPITokenDiscoverResponse struct {
	OK          bool   `json:"ok"`
	Message     string `json:"message"`
	Latency     int64  `json:"latency"`
	AccessToken string `json:"accessToken,omitempty"`
	UserID      int    `json:"userId,omitempty"`
}

type sub2apiGroupsResponse struct {
	OK      bool                 `json:"ok"`
	Message string               `json:"message"`
	Latency int64                `json:"latency"`
	Groups  []sub2apiGroupOption `json:"groups"`
}

type sub2apiGroupOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Platform    string `json:"platform"`
	Status      string `json:"status"`
}

func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var body integrationTestRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	source := normalizeSyncSource(body.Source)
	switch source {
	case "newapi":
		result, err := s.testNewAPIConnection(r, body.NewAPI)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	case "sub2api":
		result, err := s.testSub2APIConnection(r, body.Sub2API)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		result, err := s.testCPAConnection(r, body.CPA)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func (s *Server) handleNewAPITokenDiscover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NewAPI integrationNewAPIConfig `json:"newapi"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	startedAt := time.Now()
	client := newapi.New(
		body.NewAPI.BaseURL,
		body.NewAPI.Username,
		body.NewAPI.Password,
		body.NewAPI.AccessToken,
		body.NewAPI.UserID,
		body.NewAPI.SessionCookie,
		probeTimeout(body.NewAPI.RequestTimeout),
		s.cfg.SyncProxyURL(),
	)
	token, userID, err := client.GenerateAccessToken(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, newAPITokenDiscoverResponse{
		OK:          true,
		Message:     "已生成并回填 NewAPI Access Token。再次点击会让远端生成一个新的 token。",
		Latency:     time.Since(startedAt).Milliseconds(),
		AccessToken: token,
		UserID:      userID,
	})
}

func (s *Server) handleSub2APIGroups(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sub2API integrationSub2APIConfig `json:"sub2api"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	startedAt := time.Now()
	client := sub2api.New(
		body.Sub2API.BaseURL,
		body.Sub2API.Email,
		body.Sub2API.Password,
		body.Sub2API.APIKey,
		body.Sub2API.GroupID,
		probeTimeout(body.Sub2API.RequestTimeout),
		s.cfg.SyncProxyURL(),
	)
	groups, err := client.ListGroups(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	sort.Slice(groups, func(i, j int) bool {
		leftActive := strings.EqualFold(groups[i].Status, "active")
		rightActive := strings.EqualFold(groups[j].Status, "active")
		if leftActive != rightActive {
			return leftActive
		}
		return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
	})

	items := make([]sub2apiGroupOption, 0, len(groups))
	for _, group := range groups {
		items = append(items, sub2apiGroupOption{
			ID:          fmt.Sprintf("%d", group.ID),
			Name:        strings.TrimSpace(group.Name),
			Description: strings.TrimSpace(group.Description),
			Platform:    strings.TrimSpace(group.Platform),
			Status:      strings.TrimSpace(group.Status),
		})
	}

	writeJSON(w, http.StatusOK, sub2apiGroupsResponse{
		OK:      true,
		Message: fmt.Sprintf("已拉取 %d 个 Sub2API 分组。", len(items)),
		Latency: time.Since(startedAt).Milliseconds(),
		Groups:  items,
	})
}

func (s *Server) testCPAConnection(r *http.Request, cfg integrationCPAConfig) (*integrationTestResponse, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("CPA Base URL 不能为空")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("CPA API Key 不能为空")
	}

	transport, err := outboundproxy.NewHTTPTransport(s.cfg.SyncProxyURL())
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: probeTimeout(cfg.RequestTimeout), Transport: transport}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	req.Header.Set("User-Agent", "chatgpt2api-studio integration test")

	startedAt := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		return &integrationTestResponse{
			OK:      false,
			Source:  "cpa",
			Message: err.Error(),
			Status:  0,
			Latency: latency,
		}, nil
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	message := fmt.Sprintf("CPA 测试完成，HTTP %d。", resp.StatusCode)
	if !ok {
		message = firstNonEmpty(strings.TrimSpace(string(raw)), http.StatusText(resp.StatusCode))
	}
	return &integrationTestResponse{
		OK:      ok,
		Source:  "cpa",
		Message: message,
		Status:  resp.StatusCode,
		Latency: latency,
	}, nil
}

func (s *Server) testNewAPIConnection(r *http.Request, cfg integrationNewAPIConfig) (*integrationTestResponse, error) {
	client := newapi.New(
		cfg.BaseURL,
		cfg.Username,
		cfg.Password,
		cfg.AccessToken,
		cfg.UserID,
		cfg.SessionCookie,
		probeTimeout(cfg.RequestTimeout),
		s.cfg.SyncProxyURL(),
	)
	if !client.Configured() {
		return nil, fmt.Errorf("请至少填写 NewAPI Base URL + 用户名密码，或 Access Token + User ID")
	}

	startedAt := time.Now()
	user, err := client.GetSelf(r.Context())
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		return &integrationTestResponse{
			OK:      false,
			Source:  "newapi",
			Message: err.Error(),
			Status:  0,
			Latency: latency,
		}, nil
	}

	return &integrationTestResponse{
		OK:       true,
		Source:   "newapi",
		Message:  fmt.Sprintf("NewAPI 已连接，当前用户 %s（ID %d）。", firstNonEmpty(user.DisplayName, user.Username, user.Email), user.ID),
		Status:   http.StatusOK,
		Latency:  latency,
		UserID:   user.ID,
		Username: firstNonEmpty(user.Username, user.DisplayName),
		Email:    user.Email,
	}, nil
}

func (s *Server) testSub2APIConnection(r *http.Request, cfg integrationSub2APIConfig) (*integrationTestResponse, error) {
	client := sub2api.New(
		cfg.BaseURL,
		cfg.Email,
		cfg.Password,
		cfg.APIKey,
		cfg.GroupID,
		probeTimeout(cfg.RequestTimeout),
		s.cfg.SyncProxyURL(),
	)
	if !client.Configured() {
		return nil, fmt.Errorf("请至少填写 Sub2API Base URL + 管理邮箱密码，或 Base URL + API Key")
	}

	startedAt := time.Now()
	groups, err := client.ListGroups(r.Context())
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		return &integrationTestResponse{
			OK:      false,
			Source:  "sub2api",
			Message: err.Error(),
			Status:  0,
			Latency: latency,
		}, nil
	}

	return &integrationTestResponse{
		OK:         true,
		Source:     "sub2api",
		Message:    fmt.Sprintf("Sub2API 已连接，当前可读取 %d 个分组。", len(groups)),
		Status:     http.StatusOK,
		Latency:    latency,
		GroupCount: len(groups),
	}, nil
}

func probeTimeout(value int) time.Duration {
	timeout := value
	if timeout <= 0 {
		timeout = 20
	}
	if timeout < 5 {
		timeout = 5
	}
	return time.Duration(timeout) * time.Second
}
