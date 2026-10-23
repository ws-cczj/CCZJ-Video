// Run from the repository root: go run reports/reading-source-samples-2026-10-19/verify_catalog.go
package main

import (
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "cczjVideo/app/provider"
)

func main() {
 base := "reports/reading-source-samples-2026-10-19"
 raw, err := os.ReadFile(filepath.Join(base,"catalog.json")); must(err)
 var catalog []struct { ID, Kind string; Document json.RawMessage; Sample struct { DetailURL, ChapterURL string } }
 must(json.Unmarshal(raw,&catalog))
 fixtures:=map[string][4]string{
  "legado-kuwo":{"kuwo-http.txt","kuwo-detail.txt","kuwo-toc.txt","kuwo-content.txt"},
  "legado-yueyou":{"suixkan-search.txt","suixkan-detail.txt","suixkan-toc.txt","suixkan-content.txt"},
  "mihon-guazi":{"guazi-search.txt","guazi-detail.txt","guazi-detail.txt","guazi-pages.txt"},
  "mihon-dongman":{"dongman-search.txt","dongman-detail.txt","dongman-detail.txt","dongman-pages.txt"},
 }
 for _, c := range catalog {
  files,ok:=fixtures[c.ID];if !ok {continue}
  doc,err:=provider.ParseDocument(string(c.Document),c.Kind);must(err)
  response:=func(i int,url string)*provider.Response{b,err:=os.ReadFile(filepath.Join(base,files[i]));must(err);return &provider.Response{StatusCode:200,FinalURL:url,Text:string(b)}}
  items,_,err:=provider.ExtractItems(doc,doc.Operations.Search,response(0,c.Sample.DetailURL));must(err)
  if len(items)==0 {panic(c.ID+": empty search")}
  detail,err:=provider.ExtractDetail(doc,doc.Operations.Detail,response(1,c.Sample.DetailURL));must(err)
  chapters,_,err:=provider.ExtractChapters(doc,doc.Operations.Toc,response(2,c.Sample.DetailURL));must(err)
  if len(chapters)==0 {panic(c.ID+": empty toc")}
  if c.Kind=="novel" {text,_,err:=provider.ExtractText(doc,doc.Operations.Content,response(3,c.Sample.ChapterURL));must(err);if len([]rune(text))<1000{panic(c.ID+": truncated text")};fmt.Printf("%s search=%d title=%s chapters=%d contentCharacters=%d author=%s\n",c.ID,len(items),detail.Title,len(chapters),len([]rune(text)),detail.Author)
  } else {pages,_,err:=provider.ExtractPages(doc,doc.Operations.Pages,response(3,c.Sample.ChapterURL));must(err);if len(pages)==0{panic(c.ID+": empty pages")};fmt.Printf("%s search=%d title=%s chapters=%d pages=%d\n",c.ID,len(items),detail.Title,len(chapters),len(pages))}
 }
}
func must(err error){if err!=nil {panic(err)}}
