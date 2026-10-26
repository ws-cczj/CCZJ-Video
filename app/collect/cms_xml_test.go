package collect

import (
	"context"
	"testing"
)

func TestLegacyCMSXMLSearchAndDetail(t *testing.T) {
	for _, body := range []string{
		`<?xml version="1.0"?><rss version="5.1"><list page="1" pagecount="2" pagesize="20" recordcount="21"><video><id>103186</id><tid>9</tid><name><![CDATA[凡人 & 修仙]]></name><type>短剧</type><last>2026-10-26</last><note>更新10集</note><dl><dd flag="hls"><![CDATA[第1集$https://example.test/1.m3u8#第2集$https://example.test/2.m3u8]]></dd><dd flag="backup">第1集$https://example.test/b.m3u8</dd></dl></video></list></rss>`,
		`<rss><list page="1" pagecount="0" recordcount="0"></list><class><ty id="1">电影</ty></class></rss>`,
	} {
		withStubBody(t, body)
		result, err := FetchWithStrategy(context.Background(), CreateStrategy("https://example.test/api"), "https://example.test/api?ac=detail&wd=凡人")
		if err != nil {
			t.Fatal(err)
		}
		if result.Total.Int() == 0 {
			if len(result.List) != 0 {
				t.Fatal("empty list changed")
			}
			continue
		}
		if result.Pagecount.Int() != 2 || len(result.List) != 1 || result.List[0].VodName != "凡人 & 修仙" || result.List[0].VodId.String() != "103186" || result.List[0].VodPlayFrom != "hls$$$backup" {
			t.Fatalf("lost XML catalogue fields: %+v %+v", result, result.List)
		}
	}
}
