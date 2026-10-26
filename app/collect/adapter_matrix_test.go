package collect

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestSourceAdapterResponseMatrix(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<?xml version="1.0" encoding="gb2312"?><rss><list><video><id>1</id><name>中文片名</name></video></list></rss>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name, body string
		count      int
		failed     bool
		apiFailure bool
		declared   bool
	}{
		{name: "CMS aliases and string metadata", body: `{"code":"1","page":"1","pagecount":"2","list":[{"id":1,"title":"影片"}]}`, count: 1},
		{name: "single detail object", body: `{"code":1,"list":{"vod_id":"1","vod_name":"影片"}}`, count: 1},
		{name: "valid null list", body: `{"code":1,"list":null}`},
		{name: "business error ignores invalid list", body: `{"code":1002,"msg":"暂不支持搜索","list":"refused"}`, failed: true, apiFailure: true},
		{name: "unknown list format", body: `{"code":1,"list":"not a list"}`, failed: true},
		{name: "successful reply has no list", body: `{"code":1}`, failed: true},
		{name: "HTML verification page", body: `<!doctype html><html>Verify</html>`, failed: true},
		{name: "partial invalid identity retains valid row", body: `{"code":1,"list":[{"vod_id":"1","vod_name":"影片"},{"name":"没有标识"}]}`, count: 1},
		{name: "declared nested envelope", body: declarativeBody, declared: true, count: 2},
		{name: "declared missing identity is not success", body: `{"code":"0","data":{"list":[{"unmapped":"value"}]}}`, declared: true, failed: true},
		{name: "legacy GBK XML", body: string(gbk), count: 1},
		{name: "XML cannot hide trailing document", body: `<rss><list/></rss><error>failure</error>`, failed: true},
	} {
		t.Run(item.name, func(t *testing.T) {
			withStubBody(t, item.body)
			strategy := CreateStrategy("https://example.test/api")
			if item.declared {
				strategy = strategyFromJSON(t, "https://example.test/api", declarativeConfig)
			}
			result, err := FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{}))
			if (err != nil) != item.failed {
				t.Fatalf("result=%+v err=%v expectedFailure=%t", result, err, item.failed)
			}
			if err == nil && len(result.List) != item.count {
				t.Fatalf("count=%d want=%d", len(result.List), item.count)
			}
			if item.apiFailure {
				var remote *APIError
				if !errors.As(err, &remote) || remote.Code != 1002 {
					t.Fatalf("business refusal misclassified as decode failure: %v", err)
				}
			}
		})
	}
}
