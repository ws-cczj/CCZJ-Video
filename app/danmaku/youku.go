package danmaku

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/apperror"
)

const (
	youkuProviderID    = "youku"
	youkuSearchHost    = "search.youku.com"
	youkuCatalogHost   = "openapi.youku.com"
	youkuMTOPHost      = "acs.youku.com"
	youkuReferer       = "https://v.youku.com/"
	youkuAgent         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
	youkuSegmentMS     = 60_000
	youkuMTOPKey       = "24679788"
	youkuMessageSuffix = "MkmC9SoIw6xCkSKHhJ7b5D2r51kBiREr"
)

var youkuShowID = regexp.MustCompile(`^[A-Za-z0-9]{10,32}$`)
var youkuVideoID = regexp.MustCompile(`^X[A-Za-z0-9_-]{6,30}={0,2}$`)
var youkuToken = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

type youkuProvider struct {
	client      *Client
	mtop        *Client
	visitor     string
	sessionGate chan struct{}
	session     youkuSession
}

type youkuSession struct {
	token, encrypted string
	expires          time.Time
}

func newYoukuProvider(client *Client) *youkuProvider {
	mtop := &Client{}
	if client != nil && client.http != nil {
		httpClient := *client.http
		httpClient.Jar = nil
		httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		mtop.http = &httpClient
	}
	visitor := make([]byte, 16)
	rand.Read(visitor)
	return &youkuProvider{client: client, mtop: mtop, visitor: base64.RawStdEncoding.EncodeToString(visitor), sessionGate: make(chan struct{}, 1)}
}

func (p *youkuProvider) Search(ctx context.Context, title string) ([]Candidate, error) {
	query := url.Values{"keyword": {title}, "userAgent": {youkuAgent}, "site": {"1"}, "categories": {"0"}, "ftype": {"0"}, "ob": {"0"}, "pg": {"1"}}
	root, err := p.client.object(ctx, "https://"+youkuSearchHost+"/api/search?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	rows := arr(root["pageComponentList"])
	if rows == nil || len(rows) > 100 {
		return nil, schemaError(fmt.Errorf("invalid Youku search results"))
	}
	results := []Candidate{}
	seen := map[string]bool{}
	for _, value := range rows {
		row := obj(obj(value)["commonData"])
		id, name := text(row["showId"]), plainTitle(text(obj(row["titleDTO"])["displayName"]))
		if text(row["isYouku"]) != "1" || text(row["realShowId"]) != id || !youkuShowID.MatchString(id) ||
			name == "" || number(row["episodeTotal"]) < 1 || seen[id] {
			continue
		}
		year, metadata, _ := strings.Cut(text(row["feature"]), "·")
		kind, _, _ := strings.Cut(metadata, "·")
		kind = mediaKind(strings.TrimSpace(kind))
		if kind != "tv" && kind != "animation" && kind != "movie" {
			continue
		}
		results = append(results, Candidate{Provider: youkuProviderID, Ref: id, Title: name, Year: metadataYear(year), Kind: kind})
		seen[id] = true
	}
	return results, nil
}

func (p *youkuProvider) Episodes(ctx context.Context, series string) ([]Episode, error) {
	if !youkuShowID.MatchString(series) {
		return nil, apperror.New(apperror.Validation, "优酷作品标识无效")
	}
	const pageSize = 50
	pages := 1
	results := []Episode{}
	for page := 1; page <= pages; page++ {
		query := url.Values{"client_id": {"53e6cc67237fc59a"}, "package": {"com.huawei.hwvplayer.youku"},
			"ext": {"show"}, "show_id": {series}, "page": {strconv.Itoa(page)}, "count": {strconv.Itoa(pageSize)}}
		root, err := p.client.object(ctx, "https://"+youkuCatalogHost+"/v2/shows/videos.json?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		total, err := strconv.Atoi(text(root["total"]))
		rows := arr(root["videos"])
		if err != nil || total < 0 || rows == nil || len(rows) > pageSize {
			return nil, schemaError(fmt.Errorf("invalid Youku episode catalog"))
		}
		if total > 500 {
			return nil, apperror.New(apperror.Unsupported, "优酷目录超出单次匹配范围")
		}
		if page == 1 {
			pages = (total + pageSize - 1) / pageSize
		}
		for _, value := range rows {
			row := obj(value)
			id, title := text(row["id"]), plainTitle(text(row["title"]))
			label := text(row["seq"])
			duration, timeErr := secondsMS(text(row["duration"]))
			_, numbered := episodeOrdinal(label)
			kind := mediaKind(text(row["category"]))
			if !youkuVideoID.MatchString(id) || title == "" || !numbered || timeErr != nil || duration <= 0 || matchExtra.MatchString(title) ||
				(kind != "tv" && kind != "animation" && kind != "movie") {
				continue
			}
			results = append(results, Episode{Provider: youkuProviderID, SeriesID: series, EpisodeID: id,
				Title: title, Number: label, DurationMS: duration, PageURL: youkuReferer + "v_show/id_" + id + ".html"})
		}
	}
	return results, nil
}

func (p *youkuProvider) Index(ctx context.Context, episode Episode) (Index, error) {
	if episode.DurationMS <= 0 || episode.DurationMS > 86_400_000 {
		return Index{}, schemaError(fmt.Errorf("invalid Youku episode duration"))
	}
	return regularIndex(episode.DurationMS, youkuSegmentMS), nil
}

func (p *youkuProvider) Segment(ctx context.Context, episode Episode, segment Segment) ([]Item, int, error) {
	if !youkuShowID.MatchString(episode.SeriesID) || !youkuVideoID.MatchString(episode.EpisodeID) || !regularSegment(segment, youkuSegmentMS) {
		return nil, 0, apperror.New(apperror.Validation, "优酷弹幕分片标识无效")
	}
	session, err := p.getSession(ctx, "")
	if err != nil {
		return nil, 0, err
	}
	rows, err := p.readSegment(ctx, episode.EpisodeID, segment.StartMS/youkuSegmentMS, session)
	if apperror.Is(err, apperror.AuthenticationRequired) {
		session, err = p.getSession(ctx, session.token)
		if err == nil {
			rows, err = p.readSegment(ctx, episode.EpisodeID, segment.StartMS/youkuSegmentMS, session)
		}
	}
	if err != nil {
		return nil, 0, err
	}
	items := make([]Item, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		at, err := strconv.ParseInt(text(row["playat"]), 10, 64)
		if err != nil || at < segment.StartMS || at >= segment.EndMS {
			continue
		}
		style, _ := object([]byte(text(row["propertis"])))
		mode := 1
		switch text(style["pos"]) {
		case "1":
			mode = 5
		case "2":
			mode = 4
		}
		item := Item{ID: text(row["id"]), TimeMS: at, Text: text(row["content"]), Mode: mode,
			Color: rgbColor(text(style["color"]), 10), FontSize: 25}
		if len(item.ID) <= 128 && validItem(item) {
			items = append(items, item)
		}
	}
	return sorted(items), len(rows) - len(items), nil
}

func (p *youkuProvider) getSession(ctx context.Context, rejected string) (youkuSession, error) {
	select {
	case <-ctx.Done():
		return youkuSession{}, ctx.Err()
	case p.sessionGate <- struct{}{}:
	}
	defer func() { <-p.sessionGate }()
	if p.session.token != rejected && time.Until(p.session.expires) > 10*time.Second {
		return p.session, nil
	}
	p.session = youkuSession{}
	query := url.Values{"jsv": {"2.5.1"}, "appKey": {youkuMTOPKey}}
	var session youkuSession
	_, err := p.request(ctx, "/h5/mtop.com.youku.aplatform.weakget/1.0/", query, nil, session, func(cookies []*http.Cookie) {
		for _, cookie := range cookies {
			switch cookie.Name {
			case "_m_h5_tk":
				session.token = cookie.Value
			case "_m_h5_tk_enc":
				session.encrypted = cookie.Value
			}
		}
	})
	if err != nil {
		return youkuSession{}, err
	}
	key, until, _ := strings.Cut(session.token, "_")
	expires, err := strconv.ParseInt(until, 10, 64)
	if err != nil || !youkuToken.MatchString(key) || session.encrypted == "" || len(session.encrypted) > 1024 ||
		!time.UnixMilli(expires).After(time.Now()) {
		return youkuSession{}, apperror.New(apperror.AuthenticationRequired, "优酷未提供有效的匿名弹幕会话")
	}
	session.expires = time.Now().Add(15 * time.Minute)
	if serverExpiry := time.UnixMilli(expires); serverExpiry.Before(session.expires) {
		session.expires = serverExpiry
	}
	p.session = session
	return session, nil
}

type youkuSegmentMessage struct {
	Time          int64  `json:"ctime"`
	ClientType    int    `json:"ctype"`
	ClientVersion string `json:"cver"`
	Visitor       string `json:"guid"`
	Segment       int64  `json:"mat"`
	Count         int    `json:"mcount"`
	Playlist      int    `json:"pid"`
	Version       string `json:"sver"`
	Type          int    `json:"type"`
	Video         string `json:"vid"`
	Message       string `json:"msg,omitempty"`
	Signature     string `json:"sign,omitempty"`
}

func (p *youkuProvider) readSegment(ctx context.Context, vid string, index int64, session youkuSession) ([]any, error) {
	timestamp := time.Now().UnixMilli()
	message := youkuSegmentMessage{Time: timestamp, ClientType: 10004, ClientVersion: "v1.0", Visitor: p.visitor,
		Segment: index, Count: 1, Version: "3.1.0", Type: 1, Video: vid}
	raw, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	message.Message = base64.StdEncoding.EncodeToString(raw)
	message.Signature = fmt.Sprintf("%x", md5.Sum([]byte(message.Message+youkuMessageSuffix)))
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	token, _, _ := strings.Cut(session.token, "_")
	millis := strconv.FormatInt(timestamp, 10)
	sign := fmt.Sprintf("%x", md5.Sum([]byte(strings.Join([]string{token, millis, youkuMTOPKey, string(payload)}, "&"))))
	query := url.Values{"jsv": {"2.5.6"}, "appKey": {youkuMTOPKey}, "t": {millis}, "sign": {sign},
		"api": {"mopen.youku.danmu.list"}, "v": {"1.0"}, "type": {"originaljson"}, "dataType": {"jsonp"},
		"timeout": {"20000"}, "jsonpIncPrefix": {"utility"}}
	raw, err = p.request(ctx, "/h5/mopen.youku.danmu.list/1.0/", query, url.Values{"data": {string(payload)}}, session, nil)
	if err != nil {
		return nil, err
	}
	root, err := object(raw)
	if err != nil {
		return nil, err
	}
	success := false
	for _, value := range arr(root["ret"]) {
		code, _, _ := strings.Cut(text(value), "::")
		switch code {
		case "SUCCESS":
			success = true
		case "FAIL_SYS_TOKEN_EMPTY", "FAIL_SYS_TOKEN_EXOIRED", "FAIL_SYS_TOKEN_EXPIRED":
			return nil, apperror.New(apperror.AuthenticationRequired, "优酷匿名弹幕会话已失效")
		}
	}
	if !success {
		return nil, apperror.New(apperror.Unavailable, "优酷弹幕接口暂不可用")
	}
	result, err := object([]byte(text(obj(root["data"])["result"])))
	if err != nil {
		return nil, err
	}
	if err := business(result["code"], "1"); err != nil {
		return nil, err
	}
	rows := arr(obj(result["data"])["result"])
	if rows == nil || len(rows) > MaxSegmentItems {
		return nil, schemaError(fmt.Errorf("invalid Youku barrage results"))
	}
	return rows, nil
}

func (p *youkuProvider) request(ctx context.Context, path string, query, form url.Values, session youkuSession, capture func([]*http.Cookie)) ([]byte, error) {
	endpoint := url.URL{Scheme: "https", Host: youkuMTOPHost, Path: path, RawQuery: query.Encode()}
	method := http.MethodGet
	if form != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", youkuReferer)
	req.Header.Set("User-Agent", youkuAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if session.token != "" {
		req.AddCookie(&http.Cookie{Name: "_m_h5_tk", Value: session.token})
		req.AddCookie(&http.Cookie{Name: "_m_h5_tk_enc", Value: session.encrypted})
	}
	return p.mtop.readHTTPWithCookies(ctx, req, capture)
}
