package domain

import "testing"

func TestNormalizeAndImport(t *testing.T) {
	p, err := ParseImport("domain,enabled,test_url\nWWW.Twitch.TV.,true,https://www.twitch.tv/\nwww.twitch.tv,false,\nlocalhost,true,\n例子.com,true,", "csv", "r", nil)
	if err != nil || len(p.Rows) != 4 {
		t.Fatalf("%v %+v", err, p)
	}
	if p.Rows[0].Entry.Hostname != "www.twitch.tv" || p.Rows[1].State != "duplicate" || p.Rows[2].State != "invalid" || p.Rows[3].Entry.Hostname != "xn--fsqu00a.com" {
		t.Fatalf("匯入結果不正確：%+v", p.Rows)
	}
	if ValidateURL("www.twitch.tv", "https://evil.example.com/") == nil || ValidateURL("www.twitch.tv", "https://user:pass@www.twitch.tv/") == nil {
		t.Fatal("未拒絕危險測速網址")
	}
}

func TestURLImportKeepsWholeHostnameAndRequiresConflictChoice(t *testing.T) {
	existing := []DomainEntry{{ID: "old", Hostname: "cdn.example.com", Enabled: true}}
	p, err := ParseImport("https://cdn.example.com/files/test.bin?q=1\nhttps://deep.a.example.com/data.bin\nexample.com\nhttps://cdn.example.com/other.bin", "txt", "revision", existing)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0].State != "update" || p.Rows[0].Entry.ID != "old" || p.Rows[0].Entry.Hostname != "cdn.example.com" || p.Rows[0].Entry.TestURL != "https://cdn.example.com/files/test.bin?q=1" {
		t.Fatal("網址子域或完整資源遺失")
	}
	if p.Rows[1].Entry.Hostname != "deep.a.example.com" || p.Rows[2].Entry.Hostname != "example.com" || p.Rows[3].State != "conflict" {
		t.Fatal("完整 hostname 去重或衝突錯誤")
	}
	for _, value := range []string{"http://cdn.example.com/a", "https://a:b@cdn.example.com/a", "https://cdn.example.com:444/a", "https://cdn.example.com/a#fragment"} {
		v, _ := ParseImport(value, "txt", "r", nil)
		if v.Rows[0].State != "invalid" {
			t.Fatalf("不安全網址未拒：%s", value)
		}
	}
}
