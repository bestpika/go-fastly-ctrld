package controld

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

type Client struct {
	HTTP   *http.Client
	Token  func() (string, error)
	base   string
	Delay  func(context.Context, time.Duration) error
	Report func(error)
}
type envelope struct {
	Success *bool           `json:"success"`
	Body    json.RawMessage `json:"body"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func New(token func() (string, error)) *Client {
	return &Client{Token: token, base: "https://api.controld.com", HTTP: &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Delay: delay}
}
func delay(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) request(ctx context.Context, method, path string, form url.Values) (output json.RawMessage, failure error) {
	defer func() {
		if failure != nil && c.Report != nil && !(method == http.MethodGet && ctx.Err() == context.Canceled) {
			c.Report(failure)
		}
	}()
	for attempt := 0; attempt < 3; attempt++ {
		if method == http.MethodGet && ctx.Err() == context.Canceled {
			return nil, domain.Fail("CANCELLED", "讀取已取消")
		}
		token, err := c.Token()
		if err != nil {
			return nil, err
		}
		if token == "" {
			return nil, domain.Fail("AUTH_FAILED", "尚未設定 Control D 權杖")
		}
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(requestCtx, method, c.base+path, body)
		if err != nil {
			cancel()
			return nil, domain.Fail("INVALID_INPUT", "API 路徑無效")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response, err := c.HTTP.Do(req)
		status := 0
		retryAfter := time.Duration(0)
		var data []byte
		requestID := ""
		if err == nil {
			status = response.StatusCode
			data, err = io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
			response.Body.Close()
			retryAfter = parseRetryAfter(response.Header.Get("Retry-After"))
			requestID = response.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = response.Header.Get("CF-Ray")
			}
			if len(data) > 8*1024*1024 {
				err = fmt.Errorf("回應過大")
			}
		}
		cancel()
		if method == http.MethodGet && ctx.Err() == context.Canceled {
			return nil, domain.Fail("CANCELLED", "讀取已取消")
		}
		if status == 401 {
			return nil, apiError(method, path, status, "AUTH_FAILED", "Control D 認證失敗", requestID, data, token)
		}
		if status == 403 {
			return nil, apiError(method, path, status, "PERMISSION_DENIED", "Control D 權杖沒有此操作權限", requestID, data, token)
		}
		retryable := err != nil || status == 429 || status >= 500
		if method == http.MethodGet && retryable && attempt < 2 && ctx.Err() == nil {
			wait := time.Duration(200*(1<<attempt)+rand.IntN(100)) * time.Millisecond
			if retryAfter > 30*time.Second {
				return nil, apiError(method, path, status, "RATE_LIMITED", "Control D 要求延後重試", requestID, data, token)
			}
			if retryAfter > wait {
				wait = retryAfter
			}
			if err = c.Delay(ctx, wait); err != nil {
				if ctx.Err() == context.Canceled {
					return nil, domain.Fail("CANCELLED", "API 讀取已取消")
				}
				return nil, apiError(method, path, status, "API_UNKNOWN", "API 讀取逾時或重試中斷", requestID, data, token)
			}
			continue
		}
		if err != nil {
			return nil, apiError(method, path, status, "API_UNKNOWN", "請求中斷或回應無效；寫入須先回讀", requestID, data, token)
		}
		if status == 429 {
			return nil, apiError(method, path, status, "RATE_LIMITED", "Control D 請求超過速率限制", requestID, data, token)
		}
		if status < 200 || status >= 300 {
			return nil, apiError(method, path, status, "API_REJECTED", "Control D 拒絕此請求", requestID, data, token)
		}
		var result envelope
		if json.Unmarshal(data, &result) != nil || result.Success == nil {
			return nil, apiError(method, path, status, "API_UNKNOWN", "Control D 回應格式無法確認", requestID, data, token)
		}
		if !*result.Success {
			return nil, apiError(method, path, status, "API_REJECTED", "Control D 回報操作失敗", requestID, data, token)
		}
		return result.Body, nil
	}
	return nil, domain.Fail("API_UNKNOWN", "API 讀取無法確認")
}

func apiError(method, path string, status int, code, message, requestID string, body []byte, token string) *domain.Error {
	op := "設定檔清單"
	switch {
	case strings.Contains(path, "/groups"):
		op = "資料夾清單"
		if method == http.MethodPost {
			op = "建立一般資料夾"
		}
	case strings.Contains(path, "/rules/"):
		op = "讀取資料夾規則"
		if method == http.MethodDelete {
			op = "刪除指定規則"
		}
	case strings.HasSuffix(path, "/rules"):
		op = "寫入指定規則"
		if method == http.MethodGet {
			op = "讀取根目錄規則"
		}
	}
	var vendor struct {
		Error struct {
			Code json.RawMessage `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &vendor)
	v := strings.Trim(string(vendor.Error.Code), "\"")
	if !regexp.MustCompile(`^(?:[0-9]{1,10}|[A-Z_]{1,40})$`).MatchString(v) || v == token {
		v = ""
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,96}$`).MatchString(requestID) || strings.Contains(requestID, token) {
		requestID = ""
	}
	parts := strings.Split(path, "/")
	if len(parts) > 2 && parts[1] == "profiles" && parts[2] != "" {
		if strings.Contains(requestID, parts[2]) {
			requestID = ""
		}
		if v == parts[2] {
			v = ""
		}
	}
	label := op + "：" + message
	if status > 0 {
		label += fmt.Sprintf("，HTTP %d", status)
	}
	return &domain.Error{Code: code, Message: label, Source: "controld", Operation: method + " " + op, HTTPStatus: status, VendorCode: v, RequestID: requestID, Detail: "請核對此階段的帳號權限、Profile 與資料夾。未保存權杖、Profile 識別或原始回應；讀取失敗不視為空規則。Request ID 可供支援查詢。"}
}

// 一般資料夾不送 do/status，不假造 BYPASS 或變更整夾繼承動作。
func (c *Client) CreateGroup(ctx context.Context, profile, name string) (domain.Group, error) {
	path, err := profilePath(profile)
	if err != nil {
		return domain.Group{}, err
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 1 || len([]rune(name)) > 64 || strings.ContainsAny(name, "\r\n\x00") {
		return domain.Group{}, domain.Fail("INVALID_INPUT", "資料夾名稱須為 1 至 64 字，不能含控制字元")
	}
	before, err := c.Groups(ctx, profile)
	if err != nil {
		return domain.Group{}, err
	}
	for _, g := range before {
		if g.Name == name {
			return domain.Group{}, domain.Fail("CONFLICT", "同名資料夾已存在，請直接選擇")
		}
	}
	_, writeErr := c.request(ctx, http.MethodPost, path+"/groups", url.Values{"name": {name}})
	readCtx, readCancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer readCancel()
	after, readErr := c.Groups(readCtx, profile)
	if readErr != nil {
		return domain.Group{}, domain.Fail("STATE_RECOVERY_REQUIRED", "新增資料夾後無法確認；請先重新讀取，勿重複新增")
	}
	if e, ok := writeErr.(*domain.Error); ok && e.Code != "API_UNKNOWN" {
		return domain.Group{}, writeErr
	}
	old := map[int]bool{}
	for _, g := range before {
		old[g.ID] = true
	}
	var matches []domain.Group
	for _, g := range after {
		if !old[g.ID] && g.Name == name && !g.Action {
			matches = append(matches, g)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if writeErr != nil {
		return domain.Group{}, writeErr
	}
	return domain.Group{}, domain.Fail("SCHEMA_UNSUPPORTED", "新增後未能確認一般資料夾；不套用規則，也不重送")
}

func parseRetryAfter(value string) time.Duration {
	if n, e := strconv.Atoi(value); e == nil && n > 0 {
		if n > 30 {
			return 31 * time.Second
		}
		return time.Duration(n) * time.Second
	}
	if t, e := http.ParseTime(value); e == nil {
		return max(0, time.Until(t))
	}
	return 0
}
func profilePath(id string) (string, error) {
	if !identifier.MatchString(id) {
		return "", domain.Fail("INVALID_INPUT", "Profile ID 無效")
	}
	return "/profiles/" + url.PathEscape(id), nil
}

func (c *Client) ListProfiles(ctx context.Context) ([]domain.Profile, error) {
	body, err := c.request(ctx, http.MethodGet, "/profiles", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Profiles []struct {
			ID   string `json:"PK"`
			Name string `json:"name"`
		} `json:"profiles"`
	}
	if json.Unmarshal(body, &response) != nil || response.Profiles == nil {
		return nil, domain.Fail("SCHEMA_UNSUPPORTED", "Profile 清單格式無法確認")
	}
	result := []domain.Profile{}
	seen := map[string]bool{}
	for _, p := range response.Profiles {
		if !identifier.MatchString(p.ID) || seen[p.ID] {
			return nil, domain.Fail("SCHEMA_UNSUPPORTED", "Profile ID 重複或無效")
		}
		seen[p.ID] = true
		result = append(result, domain.Profile{ID: p.ID, Name: p.Name})
	}
	return result, nil
}

func (c *Client) Groups(ctx context.Context, profile string) ([]domain.Group, error) {
	path, err := profilePath(profile)
	if err != nil {
		return nil, err
	}
	body, err := c.request(ctx, http.MethodGet, path+"/groups", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Groups []struct {
			ID     int                        `json:"PK"`
			Name   string                     `json:"group"`
			Action map[string]json.RawMessage `json:"action"`
		} `json:"groups"`
	}
	if json.Unmarshal(body, &response) != nil || response.Groups == nil || len(response.Groups) > 1000 {
		return nil, domain.Fail("SCHEMA_UNSUPPORTED", "資料夾清單格式無法確認")
	}
	result := []domain.Group{}
	seen := map[int]bool{}
	for _, g := range response.Groups {
		if g.ID <= 0 || seen[g.ID] || g.Action == nil {
			return nil, domain.Fail("SCHEMA_UNSUPPORTED", "資料夾格式無法確認")
		}
		seen[g.ID] = true
		_, action := g.Action["do"]
		result = append(result, domain.Group{ID: g.ID, Name: g.Name, Action: action})
	}
	return result, nil
}

func (c *Client) Rules(ctx context.Context, profile string) (map[string]domain.Rule, []domain.Group, error) {
	groups, err := c.Groups(ctx, profile)
	if err != nil {
		return nil, nil, err
	}
	folders := []int{0}
	for _, g := range groups {
		folders = append(folders, g.ID)
	}
	result := map[string]domain.Rule{}
	for _, folder := range folders {
		values, err := c.FolderRules(ctx, profile, folder)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range values {
			if r.Group != folder {
				return nil, nil, domain.Fail("SCHEMA_UNSUPPORTED", "規則資料夾與查詢不一致")
			}
			result[r.Hostname] = r
			if len(result) > 10000 {
				return nil, nil, domain.Fail("SCHEMA_UNSUPPORTED", "規則數超出支援上限")
			}
		}
	}
	return result, groups, nil
}

// 清單只讀所選資料夾；跨夾安全比對另由 Rules 完整讀取。
func (c *Client) FolderRules(ctx context.Context, profile string, folder int) ([]domain.Rule, error) {
	path, err := profilePath(profile)
	if err != nil {
		return nil, err
	}
	if folder < 0 {
		return nil, domain.Fail("INVALID_INPUT", "資料夾無效")
	}
	endpoint := path + "/rules"
	if folder != 0 {
		endpoint += "/" + strconv.Itoa(folder)
	}
	body, err := c.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Rules []json.RawMessage `json:"rules"`
	}
	if json.Unmarshal(body, &response) != nil || response.Rules == nil || len(response.Rules) > 10000 {
		return nil, domain.Fail("SCHEMA_UNSUPPORTED", "規則清單格式無法確認")
	}
	result := []domain.Rule{}
	seen := map[string]bool{}
	for _, raw := range response.Rules {
		r, err := ParseRule(raw)
		if err != nil {
			return nil, err
		}
		if r.Group != folder || seen[r.Hostname] {
			return nil, domain.Fail("SCHEMA_UNSUPPORTED", "規則資料夾不符或網域重複")
		}
		seen[r.Hostname] = true
		result = append(result, r)
	}
	return result, nil
}

func (c *Client) Write(ctx context.Context, profile string, r domain.Rule, create bool) error {
	path, err := profilePath(profile)
	if err != nil {
		return err
	}
	hostname, err := domain.Normalize(r.Hostname)
	if err != nil || hostname != r.Hostname || r.Do < 0 || r.Do > 3 || r.Status < 0 || r.Status > 1 || r.Group < 0 || len([]rune(r.Comment)) > 64 {
		return domain.Fail("INVALID_INPUT", "規則欄位無效")
	}
	if !r.Safe {
		return domain.Fail("SNAPSHOT_UNSUPPORTED", "規則未確認可安全寫回")
	}
	form := url.Values{"hostnames[]": {r.Hostname}, "do": {strconv.Itoa(r.Do)}, "status": {strconv.Itoa(r.Status)}, "group": {strconv.Itoa(r.Group)}, "comment": {r.Comment}}
	form.Set("via", r.Via)
	if r.Do == 2 {
		form.Set("via_v6", r.ViaV6)
	} else if r.ViaV6 != "" {
		return domain.Fail("SNAPSHOT_UNSUPPORTED", "非指定目標規則含不支援的 IPv6 目標")
	}
	method := http.MethodPut
	if create {
		method = http.MethodPost
	}
	_, err = c.request(ctx, method, path+"/rules", form)
	return err
}

func (c *Client) Delete(ctx context.Context, profile, hostname string) error {
	path, err := profilePath(profile)
	if err != nil {
		return err
	}
	normal, err := domain.Normalize(hostname)
	if err != nil || normal != hostname {
		return domain.Fail("INVALID_INPUT", "刪除網域無效")
	}
	_, err = c.request(ctx, http.MethodDelete, path+"/rules/"+url.PathEscape(hostname), nil)
	return err
}

func ParseRule(raw json.RawMessage) (domain.Rule, error) {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || top == nil {
		return domain.Rule{}, domain.Fail("SCHEMA_UNSUPPORTED", "規則不是 JSON 物件")
	}
	r := domain.Rule{Raw: bytes.Clone(raw), Safe: true}
	var hostname string
	if json.Unmarshal(top["PK"], &hostname) != nil || hostname == "" {
		return r, domain.Fail("SCHEMA_UNSUPPORTED", "規則缺少網域")
	}
	r.Hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	var action map[string]json.RawMessage
	if decodeInt(top["group"], &r.Group) != nil || json.Unmarshal(top["action"], &action) != nil || action == nil || decodeInt(action["do"], &r.Do) != nil || decodeInt(action["status"], &r.Status) != nil {
		return r, domain.Fail("SCHEMA_UNSUPPORTED", "規則必要欄位不完整")
	}
	unsafe := func(message string) {
		r.Safe = false
		if r.UnsafeReason == "" {
			r.UnsafeReason = message
		}
	}
	if r.Do < 0 || r.Do > 3 || r.Status < 0 || r.Status > 1 || r.Group < 0 {
		unsafe("規則動作或狀態不支援")
	}
	if v, ok := action["via"]; ok && decodeString(v, &r.Via) != nil {
		unsafe("目標格式不支援")
	}
	if v, ok := action["via_v6"]; ok {
		if decodeString(v, &r.ViaV6) != nil {
			unsafe("IPv6 欄位格式不支援，不能保證保留")
		} else if r.Do != 2 && r.ViaV6 != "" {
			unsafe("非指定目標規則含不支援的 IPv6 目標")
		}
	} else if r.Do == 2 {
		unsafe("指定目標規則未明確提供 IPv6 欄位，不能保證保留")
	}
	if v, ok := top["comment"]; !ok || decodeString(v, &r.Comment) != nil {
		unsafe("回應未明確提供註解，不能保證保留")
	}
	for key := range top {
		if key != "PK" && key != "order" && key != "group" && key != "action" && key != "comment" {
			unsafe("回應含尚未確認的規則欄位")
		}
	}
	for key := range action {
		if key != "do" && key != "status" && key != "via" && key != "via_v6" {
			unsafe("回應含尚未確認的動作欄位")
		}
	}
	if len([]rune(r.Comment)) > 64 {
		unsafe("註解超出可寫回長度")
	}
	return r, nil
}

func decodeInt(raw json.RawMessage, target *int) error {
	var value *int
	err := json.Unmarshal(raw, &value)
	if err != nil || value == nil {
		return fmt.Errorf("整數欄位缺少或為 null")
	}
	*target = *value
	return nil
}
func decodeString(raw json.RawMessage, target *string) error {
	var value *string
	err := json.Unmarshal(raw, &value)
	if err != nil || value == nil {
		return fmt.Errorf("字串欄位缺少或為 null")
	}
	*target = *value
	return nil
}
