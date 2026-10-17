package controld

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func mock(t *testing.T, h http.HandlerFunc) *Client {
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	c := New(func() (string, error) { return "secret", nil })
	c.base = server.URL
	c.Delay = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestFolderRulesRequestsOnlySelectedFolder(t *testing.T) {
	for _, folder := range []int{0, 7} {
		t.Run(strconv.Itoa(folder), func(t *testing.T) {
			calls := 0
			path := "/profiles/profile/rules"
			if folder != 0 {
				path += "/" + strconv.Itoa(folder)
			}
			c := mock(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != path {
					t.Errorf("讀到非所選資料夾：%s", r.URL.Path)
				}
				_, _ = fmt.Fprintf(w, `{"success":true,"body":{"rules":[{"PK":"cdn.example.com","group":%d,"action":{"do":2,"status":1,"via":"151.101.0.49","via_v6":""},"comment":""}]}}`, folder)
			})
			rules, err := c.FolderRules(context.Background(), "profile", folder)
			if err != nil || len(rules) != 1 || rules[0].Group != folder || calls != 1 {
				t.Fatalf("非單一資料夾讀取：%+v %v calls=%d", rules, err, calls)
			}
		})
	}
}

func TestReadCancellationStopsHTTPAndDoesNotLogOrRetry(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var reports, calls atomic.Int32
	c := mock(t, func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-r.Context().Done()
		close(stopped)
	})
	c.Report = func(error) { reports.Add(1) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.FolderRules(ctx, "profile", 7); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		e, ok := err.(*domain.Error)
		if !ok || e.Code != "CANCELLED" {
			t.Fatalf("取消被當 API 故障：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消未傳到 HTTP")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("伺服器連線未取消")
	}
	if calls.Load() != 1 || reports.Load() != 0 {
		t.Fatal("取消仍重試或記入錯誤")
	}
}

func TestRulesUsesOfficialRootRouteAndFolderOrder(t *testing.T) {
	var paths []string
	c := mock(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/profiles/profile/groups":
			_, _ = w.Write([]byte(`{"success":true,"body":{"groups":[{"PK":7,"group":"folder","action":{}}]}}`))
		case "/profiles/profile/rules":
			_, _ = w.Write([]byte(`{"success":true,"body":{"rules":[{"PK":"root.example.com","group":0,"action":{"do":2,"status":1,"via":"151.101.0.1","via_v6":""},"comment":""}]}}`))
		case "/profiles/profile/rules/7":
			_, _ = w.Write([]byte(`{"success":true,"body":{"rules":[{"PK":"folder.example.com","group":7,"action":{"do":2,"status":1,"via":"151.101.0.2","via_v6":""},"comment":""}]}}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":40401}}`))
		}
	})
	rules, groups, err := c.Rules(context.Background(), "profile")
	if err != nil || len(rules) != 2 || len(groups) != 1 || rules["root.example.com"].Group != 0 || rules["folder.example.com"].Group != 7 {
		t.Fatalf("路由契約錯誤：%v %+v", err, rules)
	}
	if !slices.Equal(paths, []string{"GET /profiles/profile/groups", "GET /profiles/profile/rules", "GET /profiles/profile/rules/7"}) {
		t.Fatalf("未依資料夾、根目錄、一般資料夾順序：%v", paths)
	}
}

func TestRootReadFailureStopsAndKeepsCorrectDiagnostic(t *testing.T) {
	requests := 0
	c := mock(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if strings.HasSuffix(r.URL.Path, "/groups") {
			_, _ = w.Write([]byte(`{"success":true,"body":{"groups":[{"PK":7,"group":"folder","action":{}}]}}`))
			return
		}
		if r.URL.Path != "/profiles/profile/rules" {
			t.Errorf("不應讀 /rules/0 或繼續下一夾：%s", r.URL.Path)
		}
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":40401}}`))
	})
	rules, groups, err := c.Rules(context.Background(), "profile")
	e, ok := err.(*domain.Error)
	if !ok || rules != nil || groups != nil || requests != 2 || e.Operation != "GET 讀取根目錄規則" || e.HTTPStatus != 404 || e.VendorCode != "40401" {
		t.Fatalf("讀錯被當空清單或診斷錯誤：%+v", err)
	}
	write := apiError(http.MethodPost, "/profiles/profile/rules", 400, "API_REJECTED", "拒絕", "", nil, "secret")
	if write.Operation != "POST 寫入指定規則" {
		t.Fatal("根目錄讀寫診斷混淆")
	}
}

func TestRulesAggregationDoesNotRejectHostnameAcrossFolders(t *testing.T) {
	c := mock(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			_, _ = w.Write([]byte(`{"success":true,"body":{"groups":[{"PK":7,"group":"folder","action":{}}]}}`))
			return
		}
		folder := 0
		if strings.HasSuffix(r.URL.Path, "/7") {
			folder = 7
		}
		_, _ = fmt.Fprintf(w, `{"success":true,"body":{"rules":[{"PK":"cdn.example.com","group":%d,"action":{"do":2,"status":1,"via":"151.101.0.2","via_v6":""},"comment":""}]}}`, folder)
	})
	if _, _, err := c.Rules(context.Background(), "profile"); err != nil {
		t.Fatalf("跨資料夾彙整仍因網域名稱重複中止：%v", err)
	}
}

func TestRejectedRequestContainsSafeStageAndNoProfileTokenOrBody(t *testing.T) {
	c := mock(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "request-123")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"code":123,"message":"secret profile-private https://host/path?signed=secret"}}`))
	})
	_, err := c.Groups(context.Background(), "profile-private")
	e, ok := err.(*domain.Error)
	if !ok || e.Code != "API_REJECTED" || e.HTTPStatus != 400 || e.Operation != "GET 資料夾清單" || e.VendorCode != "123" || e.RequestID != "request-123" {
		t.Fatalf("欠診斷：%+v", err)
	}
	b, _ := json.Marshal(e)
	for _, private := range []string{"secret", "profile-private", "signed="} {
		if strings.Contains(string(b), private) {
			t.Fatal("錯誤洩漏原內容")
		}
	}
}

func TestCreateNormalFolderDoesNotSendSharedActionAndReadsBack(t *testing.T) {
	created := false
	posts := 0
	c := mock(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			_ = r.ParseForm()
			if r.Form.Get("name") != "新資料夾" || r.Form.Has("do") || r.Form.Has("status") {
				t.Error("一般資料夾被加上共用動作")
			}
			created = true
			_, _ = w.Write([]byte(`{"success":true,"body":{}}`))
			return
		}
		if created {
			_, _ = w.Write([]byte(`{"success":true,"body":{"groups":[{"PK":4,"group":"新資料夾","action":{}}]}}`))
		} else {
			_, _ = w.Write([]byte(`{"success":true,"body":{"groups":[]}}`))
		}
	})
	g, err := c.CreateGroup(context.Background(), "profile", "新資料夾")
	if err != nil || g.ID != 4 || g.Action || posts != 1 {
		t.Fatalf("新增或回讀錯：%+v %v", g, err)
	}
}

func TestHTTP200FailureAndNoBlindWriteRetry(t *testing.T) {
	calls := 0
	c := mock(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"success":false,"body":[],"error":{"message":"secret"}}`))
	})
	_, err := c.ListProfiles(context.Background())
	if err == nil || err.Error() == "secret" {
		t.Fatal("沒有處理 success:false")
	}
	calls = 0
	r := domain.Rule{Hostname: "www.twitch.tv", Do: 2, Status: 1, Via: "151.101.0.214", Safe: true}
	if c.Write(context.Background(), "profile", r, true) == nil || calls != 1 {
		t.Fatal("寫入盲目重試")
	}
}

func TestFormsAndProfiles(t *testing.T) {
	c := mock(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("缺認證")
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"success":true,"body":{"profiles":[{"PK":"profile","name":"測試"}]}}`))
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("hostnames[]") != "www.twitch.tv" || r.Form.Get("do") != "2" || r.Form.Get("via") != "151.101.0.214" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("表單不正確")
		}
		_, _ = w.Write([]byte(`{"success":true,"body":{}}`))
	})
	profiles, err := c.ListProfiles(context.Background())
	if err != nil || len(profiles) != 1 {
		t.Fatal(err)
	}
	if err = c.Write(context.Background(), "profile", domain.Rule{Hostname: "www.twitch.tv", Do: 2, Status: 1, Via: "151.101.0.214", Safe: true}, true); err != nil {
		t.Fatal(err)
	}
}

func TestCNAMEWriteRequiresExplicitSuccessAndNeverRetries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		ok     bool
	}{
		{"成功", 200, `{"success":true,"body":{}}`, true},
		{"拒絕", 200, `{"success":false}`, false},
		{"缺少成功欄位", 200, `{"body":{}}`, false},
		{"未授權", 401, `{"success":false}`, false},
		{"無權限", 403, `{"success":false}`, false},
		{"限流", 429, `{"success":false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := mock(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				_ = r.ParseForm()
				if r.Method != http.MethodPut || r.Form.Get("via") != "edge.fastly.net" || r.Form.Get("hostnames[]") != "cdn.example.com" {
					t.Error("原始 CNAME 表單不正確")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			err := c.Write(context.Background(), "profile", domain.Rule{Hostname: "cdn.example.com", Do: 2, Status: 1, Via: "edge.fastly.net", Safe: true}, false)
			if (err == nil) != tc.ok || calls != 1 {
				t.Fatal("成功判定錯誤或重送寫入")
			}
		})
	}
}

func TestRuleSafetyRequiresCompleteFields(t *testing.T) {
	complete := json.RawMessage(`{"PK":"www.twitch.tv","order":1,"group":0,"comment":"保留","action":{"do":2,"status":1,"via":"151.101.0.214","via_v6":""}}`)
	r, err := ParseRule(complete)
	if err != nil || !r.Safe || r.Comment != "保留" {
		t.Fatal("完整規則錯誤")
	}
	r, err = ParseRule(json.RawMessage(`{"PK":"www.twitch.tv","order":1,"group":0,"action":{"do":2,"status":1,"via":"151.101.0.214"}}`))
	if err != nil || r.Safe || len(r.Raw) == 0 {
		t.Fatal("未阻擋不完整快照")
	}
}

func TestRateLimitReadRetriesOnly(t *testing.T) {
	calls := 0
	c := mock(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"body":{"profiles":[]}}`))
	})
	if _, err := c.ListProfiles(context.Background()); err != nil || calls != 3 {
		t.Fatal("未按規則重試")
	}
}

func TestIPv6PresenceDependsOnRuleActionAndKeepsUnsafeFieldsBlocked(t *testing.T) {
	for _, do := range []int{0, 1, 2, 3} {
		for _, suffix := range []string{"", `,"via_v6":""`, `,"via_v6":null`, `,"via_v6":"2a04:4e42::49"`, `,"unknown":true`} {
			raw := json.RawMessage(fmt.Sprintf(`{"PK":"assets.twitch.tv","group":0,"comment":"保留註解","action":{"do":%d,"status":1,"via":"HND"%s}}`, do, suffix))
			r, err := ParseRule(raw)
			wantSafe := suffix == `,"via_v6":""` || suffix == "" && do != 2 || suffix == `,"via_v6":"2a04:4e42::49"` && do == 2
			if err != nil || r.Safe != wantSafe || string(r.Raw) != string(raw) || r.Comment != "保留註解" || r.Via != "HND" {
				t.Fatalf("動作 %d 欄位 %s 安全性／原文錯誤：%+v %v", do, suffix, r, err)
			}
		}
	}
	for _, comment := range []string{"", `,"comment":null`, `,"comment":42`} {
		r, err := ParseRule(json.RawMessage(`{"PK":"assets.twitch.tv","group":0,"action":{"do":3,"status":1,"via":"HND"}` + comment + `}`))
		if err != nil || r.Safe {
			t.Fatal("缺少或無法還原的註解被當空字串")
		}
	}
}

func TestWriteSendsIPv6OnlyForSpoofAndPreservesProxyRestore(t *testing.T) {
	for _, do := range []int{0, 1, 2, 3} {
		calls := 0
		c := mock(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			_ = r.ParseForm()
			if r.Method != http.MethodPut || r.Form.Get("do") != strconv.Itoa(do) || r.Form.Has("via_v6") != (do == 2) || r.Form.Get("via") != "HND" || r.Form.Get("comment") != "保留註解" || r.Form.Get("group") != "7" {
				t.Errorf("還原表單錯誤：%v", r.Form)
			}
			_, _ = io.WriteString(w, `{"success":true,"body":{}}`)
		})
		r := domain.Rule{Hostname: "assets.twitch.tv", Do: do, Status: 1, Via: "HND", Group: 7, Comment: "保留註解", Safe: true}
		if err := c.Write(context.Background(), "profile", r, false); err != nil || calls != 1 {
			t.Fatal("安全還原失敗", err)
		}
		if do != 2 {
			r.ViaV6 = "2a04:4e42::49"
			if c.Write(context.Background(), "profile", r, false) == nil || calls != 1 {
				t.Fatal("非指定目標的未知 IPv6 語意仍送出")
			}
		}
	}
}
