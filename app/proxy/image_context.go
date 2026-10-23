package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"sync/atomic"
)

const maxImageSessions = 128

// imageSessions bounds per-source/configuration cookie ownership. Transport is
// shared; each session has its own jar. Evicting an idle entry never closes I/O.
type imageSessions struct {
	mu      sync.Mutex
	clock   uint64
	entries map[string]*imageSession
}

type imageSession struct {
	client   *http.Client
	stateful atomic.Bool
	revision atomic.Uint64
	used     uint64 // protected by imageSessions.mu
}

type imageCookieJar struct {
	http.CookieJar
	stateful *atomic.Bool
	revision *atomic.Uint64
}

func (j *imageCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if len(cookies) != 0 {
		j.stateful.Store(true)
		j.revision.Add(1)
	}
	j.CookieJar.SetCookies(u, cookies)
}

func imageContextKey(source string, headers http.Header) string {
	encoded, _ := json.Marshal(struct {
		Version int
		Source  string
		Headers http.Header
	}{2, source, headers})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (s *imageSessions) get(key string, template *http.Client) *imageSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock++
	if current := s.entries[key]; current != nil {
		current.used = s.clock
		return current
	}
	if s.entries == nil {
		s.entries = make(map[string]*imageSession)
	}
	if len(s.entries) >= maxImageSessions {
		var oldest string
		var age uint64 = ^uint64(0)
		for candidate, entry := range s.entries {
			if entry.used < age {
				oldest, age = candidate, entry.used
			}
		}
		delete(s.entries, oldest)
	}
	client := *template
	current := &imageSession{client: &client, used: s.clock}
	jar, _ := cookiejar.New(nil)
	client.Jar = &imageCookieJar{CookieJar: jar, stateful: &current.stateful, revision: &current.revision}
	s.entries[key] = current
	return current
}
