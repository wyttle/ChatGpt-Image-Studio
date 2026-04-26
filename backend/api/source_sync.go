package api

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/newapi"
	"chatgpt2api/internal/sub2api"
)

type sourceSyncStatus struct {
	Source             string               `json:"source"`
	Label              string               `json:"label"`
	Configured         bool                 `json:"configured"`
	PullSupported      bool                 `json:"pullSupported"`
	PushSupported      bool                 `json:"pushSupported"`
	Local              int                  `json:"local"`
	Remote             int                  `json:"remote"`
	PendingPush        int                  `json:"pendingPush"`
	PendingPull        int                  `json:"pendingPull"`
	InaccessibleRemote int                  `json:"inaccessibleRemote"`
	Notes              []string             `json:"notes,omitempty"`
	LastRun            *sourceSyncRunResult `json:"lastRun,omitempty"`
}

type sourceSyncRunResult struct {
	OK           bool     `json:"ok"`
	Running      bool     `json:"running"`
	Source       string   `json:"source"`
	Direction    string   `json:"direction"`
	Imported     int      `json:"imported"`
	Exported     int      `json:"exported"`
	Skipped      int      `json:"skipped"`
	Failed       int      `json:"failed"`
	Inaccessible int      `json:"inaccessible"`
	Total        int      `json:"total"`
	Processed    int      `json:"processed"`
	Phase        string   `json:"phase,omitempty"`
	Current      string   `json:"current,omitempty"`
	Error        string   `json:"error,omitempty"`
	Notes        []string `json:"notes,omitempty"`
	StartedAt    string   `json:"started_at"`
	FinishedAt   string   `json:"finished_at"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

func (s *Server) getSourceSyncRun(source string) *sourceSyncRunResult {
	s.syncRunMu.RLock()
	defer s.syncRunMu.RUnlock()
	if run := s.syncRunCache[source]; run != nil {
		copy := *run
		copy.Notes = append([]string(nil), run.Notes...)
		return &copy
	}
	return nil
}

func (s *Server) markSourceSyncRunProgress(source string, run *sourceSyncRunResult, phase, current string) {
	if run == nil {
		return
	}
	run.Running = true
	run.Phase = strings.TrimSpace(phase)
	run.Current = strings.TrimSpace(current)
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.setSourceSyncRun(source, run)
}

func (s *Server) finishSourceSyncRun(source string, run *sourceSyncRunResult) {
	if run == nil {
		return
	}
	run.Running = false
	run.Current = ""
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	run.UpdatedAt = run.FinishedAt
	s.setSourceSyncRun(source, run)
}

func (s *Server) setSourceSyncRun(source string, run *sourceSyncRunResult) {
	s.syncRunMu.Lock()
	defer s.syncRunMu.Unlock()
	if run == nil {
		delete(s.syncRunCache, source)
		return
	}
	copy := *run
	copy.Notes = append([]string(nil), run.Notes...)
	s.syncRunCache[source] = &copy
}

func (s *Server) buildSourceSyncStatus(ctx context.Context, source string) (*sourceSyncStatus, error) {
	switch normalizeSyncSource(source) {
	case "sub2api":
		return s.buildSub2APIStatus(ctx)
	case "newapi":
		return s.buildNewAPIStatus(ctx)
	default:
		return s.buildCPAStatus(ctx)
	}
}

func buildSourceSyncProgressStatus(source string, run *sourceSyncRunResult) *sourceSyncStatus {
	normalized := normalizeSyncSource(source)
	return &sourceSyncStatus{
		Source:        normalized,
		Label:         syncSourceLabel(normalized),
		PullSupported: true,
		PushSupported: true,
		LastRun:       run,
		Notes:         []string{},
	}
}

func (s *Server) runSourceSync(ctx context.Context, source, direction string) (*sourceSyncRunResult, error) {
	switch normalizeSyncSource(source) {
	case "sub2api":
		return s.runSub2APISync(ctx, direction)
	case "newapi":
		return s.runNewAPISync(ctx, direction)
	default:
		return s.runCPASync(ctx, direction)
	}
}

func normalizeSyncSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "sub2api":
		return "sub2api"
	case "newapi", "new-api":
		return "newapi"
	default:
		return "cpa"
	}
}

func syncSourceLabel(source string) string {
	switch normalizeSyncSource(source) {
	case "sub2api":
		return "Sub2API"
	case "newapi":
		return "NewAPI"
	default:
		return "CPA"
	}
}

func normalizeSyncDirection(direction string) string {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "pull", "from_cpa", "from-cpa", "import", "sync":
		return "pull"
	default:
		return "push"
	}
}

func (s *Server) buildCPAStatus(ctx context.Context) (*sourceSyncStatus, error) {
	store := s.getStore()
	summary, err := store.SyncStatus(ctx, s.getSyncClient())
	if err != nil {
		return nil, err
	}

	status := &sourceSyncStatus{
		Source:             "cpa",
		Label:              "CPA",
		Configured:         summary.Configured,
		PullSupported:      true,
		PushSupported:      true,
		Local:              summary.Local,
		Remote:             summary.Remote,
		PendingPush:        summary.Summary["pending_upload"] + summary.Summary["remote_deleted"],
		PendingPull:        summary.Summary["remote_only"],
		InaccessibleRemote: 0,
		Notes:              []string{},
	}
	if summary.DisabledMismatch > 0 {
		status.Notes = append(status.Notes, fmt.Sprintf("有 %d 个账号的禁用状态与远端不一致。", summary.DisabledMismatch))
	}
	if summary.LastRun != nil {
		status.LastRun = &sourceSyncRunResult{
			OK:         summary.LastRun.OK,
			Running:    summary.LastRun.Running,
			Source:     "cpa",
			Direction:  summary.LastRun.Direction,
			Imported:   summary.LastRun.Downloaded,
			Exported:   summary.LastRun.Uploaded,
			Failed:     summary.LastRun.DownloadFailed + summary.LastRun.UploadFailed + summary.LastRun.DisabledAlignFailed,
			Total:      summary.LastRun.Total,
			Processed:  summary.LastRun.Processed,
			Phase:      summary.LastRun.Phase,
			Current:    summary.LastRun.Current,
			Skipped:    0,
			StartedAt:  summary.LastRun.StartedAt,
			FinishedAt: summary.LastRun.FinishedAt,
			UpdatedAt:  summary.LastRun.UpdatedAt,
		}
	}
	return status, nil
}

func (s *Server) runCPASync(ctx context.Context, direction string) (*sourceSyncRunResult, error) {
	store := s.getStore()
	startedAt := time.Now().UTC().Format(time.RFC3339)
	result, err := store.RunSync(ctx, s.getSyncClient(), direction)
	if err != nil {
		return nil, err
	}
	run := &sourceSyncRunResult{
		OK:         result.OK,
		Running:    result.Running,
		Source:     "cpa",
		Direction:  result.Direction,
		Imported:   result.Downloaded,
		Exported:   result.Uploaded,
		Failed:     result.DownloadFailed + result.UploadFailed + result.DisabledAlignFailed,
		Total:      result.Total,
		Processed:  result.Processed,
		Phase:      result.Phase,
		Current:    result.Current,
		StartedAt:  firstNonEmpty(result.StartedAt, startedAt),
		FinishedAt: result.FinishedAt,
		UpdatedAt:  result.UpdatedAt,
	}
	s.setSourceSyncRun("cpa", run)
	return run, nil
}

func (s *Server) buildSub2APIStatus(ctx context.Context) (*sourceSyncStatus, error) {
	client := s.getSub2APIClient()
	store := s.getStore()
	status := &sourceSyncStatus{
		Source:        "sub2api",
		Label:         "Sub2API",
		Configured:    client.Configured(),
		PullSupported: true,
		PushSupported: true,
		LastRun:       s.getSourceSyncRun("sub2api"),
		Notes:         []string{},
	}

	localAuths, err := store.ListLocalAuths()
	if err != nil {
		return nil, err
	}
	localAuths = filterSyncableSourceAccounts(localAuths)
	status.Local = len(localAuths)
	if !status.Configured {
		return status, nil
	}

	remoteAccounts, err := client.ExportOpenAIOAuthAccounts(ctx)
	if err != nil {
		return nil, err
	}
	status.Remote = len(remoteAccounts)

	localKeys := make(map[string]struct{}, len(localAuths))
	for _, auth := range localAuths {
		if key := accounts.AuthIdentityKey(auth.Data, auth.Provider); key != "" {
			localKeys[key] = struct{}{}
		}
	}
	remoteKeys := make(map[string]struct{}, len(remoteAccounts))
	for _, account := range remoteAccounts {
		if key := accounts.AuthIdentityKey(account.Credentials, "codex"); key != "" {
			remoteKeys[key] = struct{}{}
		}
	}
	for key := range localKeys {
		if _, ok := remoteKeys[key]; !ok {
			status.PendingPush++
		}
	}
	for key := range remoteKeys {
		if _, ok := localKeys[key]; !ok {
			status.PendingPull++
		}
	}

	return status, nil
}

func (s *Server) runSub2APISync(ctx context.Context, direction string) (run *sourceSyncRunResult, err error) {
	client := s.getSub2APIClient()
	store := s.getStore()
	startedAt := time.Now().UTC().Format(time.RFC3339)
	run = &sourceSyncRunResult{
		OK:        true,
		Running:   true,
		Source:    "sub2api",
		Direction: normalizeSyncDirection(direction),
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	}
	s.setSourceSyncRun("sub2api", run)
	defer func() {
		if run == nil || !run.Running {
			return
		}
		if err != nil {
			run.OK = false
			run.Error = err.Error()
		}
		s.finishSourceSyncRun("sub2api", run)
	}()
	if !client.Configured() {
		run.OK = false
		run.Error = "sub2api 未配置"
		s.finishSourceSyncRun("sub2api", run)
		return run, nil
	}

	if run.Direction == "pull" {
		remoteAccounts, err := client.ExportOpenAIOAuthAccounts(ctx)
		if err != nil {
			return nil, err
		}
		run.Total = len(remoteAccounts)
		s.markSourceSyncRunProgress("sub2api", run, "拉取远端账号", "")
		for _, account := range remoteAccounts {
			s.markSourceSyncRunProgress("sub2api", run, "导入远端账号", account.Name)
			imported, skipped, failures, err := s.importRemoteCredential(remoteCredentialFile(account.Name, account.Credentials))
			if err != nil {
				return nil, err
			}
			run.Imported += imported
			run.Skipped += skipped
			run.Failed += failures
			run.Processed++
			s.setSourceSyncRun("sub2api", run)
		}
	} else {
		localAuths, err := store.ListLocalAuths()
		if err != nil {
			return nil, err
		}
		localAuths = filterSyncableSourceAccounts(localAuths)
		remoteAccounts, err := client.ExportOpenAIOAuthAccounts(ctx)
		if err != nil {
			return nil, err
		}
		remoteKeys := make(map[string]struct{}, len(remoteAccounts))
		for _, account := range remoteAccounts {
			if key := accounts.AuthIdentityKey(account.Credentials, "codex"); key != "" {
				remoteKeys[key] = struct{}{}
			}
		}
		run.Total = len(localAuths)
		s.markSourceSyncRunProgress("sub2api", run, "准备推送本地账号", "")
		for _, auth := range localAuths {
			s.markSourceSyncRunProgress("sub2api", run, "检查本地账号", remoteAccountName(auth))
			key := accounts.AuthIdentityKey(auth.Data, auth.Provider)
			if key != "" {
				if _, ok := remoteKeys[key]; ok {
					run.Skipped++
					run.Processed++
					s.setSourceSyncRun("sub2api", run)
					continue
				}
			}
			result, err := client.ImportOpenAIOAuthAccounts(ctx, toSub2APIRemoteAccounts([]remoteSourceAccount{{
				Name:        remoteAccountName(auth),
				Credentials: cloneMap(auth.Data),
			}}))
			if err != nil {
				return nil, err
			}
			run.Exported += result.AccountCreated
			run.Failed += result.AccountFailed + result.ProxyFailed
			run.Notes = append(run.Notes, sub2APIImportErrorNotes(result.Errors)...)
			run.Processed++
			s.setSourceSyncRun("sub2api", run)
		}
	}

	s.finishSourceSyncRun("sub2api", run)
	return run, nil
}

func sub2APIImportErrorNotes(errors []sub2api.ImportError) []string {
	if len(errors) == 0 {
		return nil
	}
	limit := min(len(errors), 3)
	notes := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		item := errors[i]
		name := strings.TrimSpace(item.Name)
		message := strings.TrimSpace(item.Message)
		if name == "" {
			name = firstNonEmpty(item.Kind, "account")
		}
		if message == "" {
			message = "unknown error"
		}
		notes = append(notes, fmt.Sprintf("Sub2API 导入失败：%s - %s", name, message))
	}
	if len(errors) > limit {
		notes = append(notes, fmt.Sprintf("Sub2API 还有 %d 条导入错误未展示。", len(errors)-limit))
	}
	return notes
}

func (s *Server) buildNewAPIStatus(ctx context.Context) (*sourceSyncStatus, error) {
	client := s.getNewAPIClient()
	store := s.getStore()
	status := &sourceSyncStatus{
		Source:        "newapi",
		Label:         "NewAPI",
		Configured:    client.Configured(),
		PullSupported: true,
		PushSupported: true,
		LastRun:       s.getSourceSyncRun("newapi"),
		Notes:         []string{},
	}

	localAuths, err := store.ListLocalAuths()
	if err != nil {
		return nil, err
	}
	localAuths = filterSyncableSourceAccounts(localAuths)
	status.Local = len(localAuths)
	if !status.Configured {
		return status, nil
	}

	channels, err := client.ListCodexChannels(ctx)
	if err != nil {
		return nil, err
	}
	status.Remote = len(channels)

	localKeys := make(map[string]struct{}, len(localAuths))
	localNames := make(map[string]struct{}, len(localAuths))
	for _, auth := range localAuths {
		if key := accounts.AuthIdentityKey(auth.Data, auth.Provider); key != "" {
			localKeys[key] = struct{}{}
		}
		localNames[normalizeRemoteAccountName(remoteAccountName(auth))] = struct{}{}
	}
	remoteKeys := map[string]struct{}{}
	remoteNames := map[string]struct{}{}
	pendingPullSeen := map[string]struct{}{}
	for _, channel := range channels {
		info := describeNewAPIRemoteChannel(channel)
		if info.identityKey != "" {
			remoteKeys[info.identityKey] = struct{}{}
		}
		if info.normalizedName != "" {
			remoteNames[info.normalizedName] = struct{}{}
		}
		if !info.pullable {
			status.InaccessibleRemote++
			continue
		}
		if matchesNewAPIRemoteInfo(info, localKeys, localNames) {
			continue
		}
		uniqueKey := info.uniqueKey()
		if uniqueKey == "" {
			uniqueKey = fmt.Sprintf("channel::%d", channel.ID)
		}
		if _, ok := pendingPullSeen[uniqueKey]; ok {
			continue
		}
		pendingPullSeen[uniqueKey] = struct{}{}
		status.PendingPull++
	}
	for _, auth := range localAuths {
		if matchesNewAPIRemoteChannel(remoteKeys, remoteNames, auth) {
			continue
		}
		status.PendingPush++
	}

	if status.InaccessibleRemote > 0 {
		status.Notes = append(status.Notes, fmt.Sprintf("有 %d 个 NewAPI 历史 Codex 渠道缺少可直接回拉的同步元数据；当前只能用于去重，不能直接拉回本地。", status.InaccessibleRemote))
	}
	return status, nil
}

func (s *Server) runNewAPISync(ctx context.Context, direction string) (run *sourceSyncRunResult, err error) {
	client := s.getNewAPIClient()
	store := s.getStore()
	startedAt := time.Now().UTC().Format(time.RFC3339)
	run = &sourceSyncRunResult{
		OK:        true,
		Running:   true,
		Source:    "newapi",
		Direction: normalizeSyncDirection(direction),
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	}
	s.setSourceSyncRun("newapi", run)
	defer func() {
		if run == nil || !run.Running {
			return
		}
		if err != nil {
			run.OK = false
			run.Error = err.Error()
		}
		s.finishSourceSyncRun("newapi", run)
	}()
	if !client.Configured() {
		run.OK = false
		run.Error = "newapi 未配置"
		s.finishSourceSyncRun("newapi", run)
		return run, nil
	}

	if run.Direction == "pull" {
		channels, err := client.ListCodexChannels(ctx)
		if err != nil {
			return nil, err
		}
		run.Total = len(channels)
		s.markSourceSyncRunProgress("newapi", run, "拉取远端渠道", "")
		for _, channel := range channels {
			s.markSourceSyncRunProgress("newapi", run, "导入远端渠道", channel.Name)
			info := describeNewAPIRemoteChannel(channel)
			if !info.pullable {
				run.Inaccessible++
				run.Processed++
				s.setSourceSyncRun("newapi", run)
				continue
			}
			imported, skipped, failures, err := s.importRemoteCredential(
				remoteCredentialFile(remoteAccountNameFromNewAPIChannelInfo(channel, info), info.authData),
			)
			if err != nil {
				return nil, err
			}
			run.Imported += imported
			run.Skipped += skipped
			run.Failed += failures
			run.Processed++
			s.setSourceSyncRun("newapi", run)
		}
		if run.Inaccessible > 0 {
			run.Notes = append(run.Notes, "部分 NewAPI Codex 渠道没有读取到真实凭证，已跳过。")
		}
	} else {
		localAuths, err := store.ListLocalAuths()
		if err != nil {
			return nil, err
		}
		localAuths = filterSyncableSourceAccounts(localAuths)
		channels, err := client.ListCodexChannels(ctx)
		if err != nil {
			return nil, err
		}
		remoteKeys := map[string]struct{}{}
		remoteNames := map[string]struct{}{}
		for _, channel := range channels {
			info := describeNewAPIRemoteChannel(channel)
			if info.identityKey != "" {
				remoteKeys[info.identityKey] = struct{}{}
			}
			if info.normalizedName != "" {
				remoteNames[info.normalizedName] = struct{}{}
			}
		}
		run.Total = len(localAuths)
		s.markSourceSyncRunProgress("newapi", run, "准备推送本地账号", "")
		for _, auth := range localAuths {
			s.markSourceSyncRunProgress("newapi", run, "检查本地账号", remoteAccountName(auth))
			identityKey := accounts.AuthIdentityKey(auth.Data, auth.Provider)
			if matchesNewAPIRemoteChannel(remoteKeys, remoteNames, auth) {
				run.Skipped++
				run.Processed++
				s.setSourceSyncRun("newapi", run)
				continue
			}
			accountType := accounts.NormalizePlanType(firstNonEmpty(stringValue(auth.Data["chatgpt_plan_type"]), stringValue(auth.Data["plan_type"])))
			if err := client.CreateCodexChannel(ctx, remoteAccountName(auth), identityKey, accountType, auth.Data); err != nil {
				run.Failed++
			} else {
				run.Exported++
			}
			run.Processed++
			s.setSourceSyncRun("newapi", run)
		}
	}

	s.finishSourceSyncRun("newapi", run)
	return run, nil
}

type remoteSourceAccount struct {
	Name        string
	Credentials map[string]any
}

type newAPIRemoteChannelInfo struct {
	identityKey    string
	normalizedName string
	authData       map[string]any
	pullable       bool
}

func sub2apiAccountsToFiles(items []sub2api.RemoteAccount) []accounts.ImportedAuthFile {
	files := make([]accounts.ImportedAuthFile, 0, len(items))
	for _, item := range items {
		files = append(files, remoteCredentialFile(item.Name, item.Credentials))
	}
	return files
}

func toSub2APIRemoteAccounts(items []remoteSourceAccount) []sub2api.RemoteAccount {
	result := make([]sub2api.RemoteAccount, 0, len(items))
	for _, item := range items {
		result = append(result, sub2api.RemoteAccount{
			Name:        item.Name,
			Platform:    "openai",
			Type:        "oauth",
			Credentials: cloneMap(item.Credentials),
		})
	}
	return result
}

func (s *Server) importRemoteCredentials(files []accounts.ImportedAuthFile) (int, int, int, error) {
	imported, _, skipped, failures, err := s.getStore().ImportAuthFiles(files)
	if err != nil {
		return 0, 0, 0, err
	}
	return imported, len(skipped), len(failures), nil
}

func (s *Server) importRemoteCredential(file accounts.ImportedAuthFile) (int, int, int, error) {
	imported, _, skipped, failures, err := s.getStore().ImportAuthFiles([]accounts.ImportedAuthFile{file})
	if err != nil {
		return 0, 0, 0, err
	}
	return imported, len(skipped), len(failures), nil
}

func remoteCredentialFile(name string, authData map[string]any) accounts.ImportedAuthFile {
	payload, _ := json.Marshal(cloneMap(authData))
	return accounts.ImportedAuthFile{
		Name: safeSyncFileName(name),
		Data: payload,
	}
}

func safeSyncFileName(name string) string {
	stem := strings.TrimSpace(name)
	if stem == "" {
		stem = "remote-auth"
	}
	stem = strings.ReplaceAll(stem, "\\", "-")
	stem = strings.ReplaceAll(stem, "/", "-")
	stem = strings.ReplaceAll(stem, ":", "-")
	stem = strings.ReplaceAll(stem, "*", "-")
	stem = strings.ReplaceAll(stem, "?", "-")
	stem = strings.ReplaceAll(stem, "\"", "-")
	stem = strings.ReplaceAll(stem, "<", "-")
	stem = strings.ReplaceAll(stem, ">", "-")
	stem = strings.ReplaceAll(stem, "|", "-")
	if !strings.HasSuffix(strings.ToLower(stem), ".json") {
		stem += ".json"
	}
	return filepath.Base(stem)
}

func remoteAccountName(auth accounts.LocalAuth) string {
	return firstNonEmpty(auth.Email, auth.UserID, strings.TrimSuffix(auth.Name, filepath.Ext(auth.Name)), "remote-auth")
}

func remoteAccountNameFromNewAPIChannel(channel newapi.Channel) string {
	return firstNonEmpty(channel.Name, "newapi-channel")
}

func remoteAccountNameFromMetadata(fallbackName, identityKey string) string {
	return firstNonEmpty(fallbackName, strings.ReplaceAll(identityKey, "::", "-"), "newapi-channel")
}

func remoteAccountNameFromNewAPIChannelInfo(channel newapi.Channel, info newAPIRemoteChannelInfo) string {
	if info.identityKey != "" {
		return remoteAccountNameFromMetadata(channel.Name, info.identityKey)
	}
	return remoteAccountNameFromNewAPIChannel(channel)
}

func newAPIMetadataFromChannel(channel newapi.Channel) (*newapi.SyncMetadata, bool) {
	return newapi.MetadataFromChannel(channel)
}

func describeNewAPIRemoteChannel(channel newapi.Channel) newAPIRemoteChannelInfo {
	if metadata, ok := newAPIMetadataFromChannel(channel); ok {
		authData := cloneMap(metadata.AuthData)
		identityKey := strings.TrimSpace(metadata.IdentityKey)
		if identityKey == "" {
			identityKey = strings.TrimSpace(accounts.AuthIdentityKey(authData, "codex"))
		}
		return newAPIRemoteChannelInfo{
			identityKey:    identityKey,
			normalizedName: normalizeRemoteAccountName(channel.Name),
			authData:       authData,
			pullable:       strings.TrimSpace(stringValue(authData["access_token"])) != "",
		}
	}
	if identityKey := identityKeyFromNewAPIRemark(channel.Remark); identityKey != "" {
		return newAPIRemoteChannelInfo{
			identityKey:    identityKey,
			normalizedName: normalizeRemoteAccountName(channel.Name),
			pullable:       false,
		}
	}
	if normalizedName := normalizeRemoteAccountName(channel.Name); normalizedName != "" {
		return newAPIRemoteChannelInfo{
			normalizedName: normalizedName,
			pullable:       false,
		}
	}
	return newAPIRemoteChannelInfo{}
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

func identityKeyFromNewAPIRemark(value string) string {
	const prefix = "chatgpt-image-studio::"
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
}

func normalizeRemoteAccountName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func matchesNewAPIRemoteChannel(remoteKeys, remoteNames map[string]struct{}, auth accounts.LocalAuth) bool {
	if identityKey := accounts.AuthIdentityKey(auth.Data, auth.Provider); identityKey != "" {
		if _, ok := remoteKeys[identityKey]; ok {
			return true
		}
	}
	_, ok := remoteNames[normalizeRemoteAccountName(remoteAccountName(auth))]
	return ok
}

func matchesNewAPIRemoteInfo(info newAPIRemoteChannelInfo, localKeys, localNames map[string]struct{}) bool {
	if info.identityKey != "" {
		if _, ok := localKeys[info.identityKey]; ok {
			return true
		}
	}
	if info.normalizedName != "" {
		if _, ok := localNames[info.normalizedName]; ok {
			return true
		}
	}
	return false
}

func (i newAPIRemoteChannelInfo) uniqueKey() string {
	if i.identityKey != "" {
		return "identity::" + i.identityKey
	}
	if i.normalizedName != "" {
		return "name::" + i.normalizedName
	}
	return ""
}

func filterSyncableSourceAccounts(auths []accounts.LocalAuth) []accounts.LocalAuth {
	if len(auths) == 0 {
		return nil
	}
	items := make([]accounts.LocalAuth, 0, len(auths))
	for _, auth := range auths {
		if !accounts.IsSyncableAccountSourceKind(auth.SourceKind) {
			continue
		}
		items = append(items, auth)
	}
	return items
}
