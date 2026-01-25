package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

var defaultAllowedHosts = map[string]struct{}{
	"login.live.com":            {},
	"login.microsoftonline.com": {},
	"teams.live.com":            {},
}

var defaultAllowedSuffixes = []string{
	".skype.com",
	".teams.live.com",
}

type CookieStore struct {
	Jar      http.CookieJar
	recorder *recordingJar
	mu       sync.Mutex
	origins  map[string]struct{}
}

type storedCookieFile struct {
	Origins []storedCookieOrigin `json:"origins"`
}

type storedCookieOrigin struct {
	Origin  string         `json:"origin"`
	Cookies []storedCookie `json:"cookies"`
}

type storedCookie struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Domain      string `json:"domain,omitempty"`
	Path        string `json:"path,omitempty"`
	ExpiresUnix int64  `json:"expires_unix,omitempty"`
	Secure      bool   `json:"secure,omitempty"`
	HTTPOnly    bool   `json:"http_only,omitempty"`
}

type recordingJar struct {
	jar      *cookiejar.Jar
	mu       sync.Mutex
	byOrigin map[string]map[string]storedCookie
}

func NewCookieStore() (*CookieStore, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	recorder := &recordingJar{
		jar:      jar,
		byOrigin: make(map[string]map[string]storedCookie),
	}
	return &CookieStore{
		Jar:      recorder,
		recorder: recorder,
		origins:  make(map[string]struct{}),
	}, nil
}

func LoadCookieStore(path string) (*CookieStore, error) {
	store, err := NewCookieStore()
	if err != nil {
		return nil, err
	}
	if err := store.Load(path); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *CookieStore) TrackURL(u *url.URL) {
	if u == nil || u.Host == "" {
		return
	}
	origin := originFromURL(u)
	s.mu.Lock()
	s.origins[origin] = struct{}{}
	s.mu.Unlock()
}

func (s *CookieStore) TrackRequest(req *http.Request) {
	if req == nil || req.URL == nil {
		return
	}
	s.TrackURL(req.URL)
}

func (s *CookieStore) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var file storedCookieFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}

	for _, originEntry := range file.Origins {
		parsed, err := url.Parse(originEntry.Origin)
		if err != nil || parsed.Host == "" {
			continue
		}
		cookies := make([]*http.Cookie, 0, len(originEntry.Cookies))
		for _, c := range originEntry.Cookies {
			cookie := &http.Cookie{
				Name:     c.Name,
				Value:    c.Value,
				Domain:   c.Domain,
				Path:     c.Path,
				Secure:   c.Secure,
				HttpOnly: c.HTTPOnly,
			}
			if cookie.Path == "" {
				cookie.Path = "/"
			}
			if c.ExpiresUnix != 0 {
				cookie.Expires = time.Unix(c.ExpiresUnix, 0).UTC()
			}
			cookies = append(cookies, cookie)
		}
		if len(cookies) > 0 {
			s.Jar.SetCookies(parsed, cookies)
		}
		s.mu.Lock()
		s.origins[originFromURL(parsed)] = struct{}{}
		s.mu.Unlock()
	}
	return nil
}

func (s *CookieStore) Save(path string) error {
	s.mu.Lock()
	origins := make([]string, 0, len(s.origins))
	for origin := range s.origins {
		origins = append(origins, origin)
	}
	s.mu.Unlock()

	var file storedCookieFile
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" {
			continue
		}
		if !hostAllowed(parsed.Hostname()) {
			continue
		}
		originCookies := s.recorder.snapshot(origin)
		if len(originCookies) == 0 {
			continue
		}
		entry := storedCookieOrigin{Origin: origin}
		entry.Cookies = append(entry.Cookies, originCookies...)
		if len(entry.Cookies) > 0 {
			file.Origins = append(file.Origins, entry)
		}
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func hostAllowed(host string) bool {
	if host == "" {
		return false
	}
	host = strings.ToLower(host)
	if _, ok := defaultAllowedHosts[host]; ok {
		return true
	}
	for _, suffix := range defaultAllowedSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func originFromURL(u *url.URL) string {
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + u.Host
}

func (r *recordingJar) Cookies(u *url.URL) []*http.Cookie {
	return r.jar.Cookies(u)
}

func (r *recordingJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	r.record(u, cookies)
	r.jar.SetCookies(u, cookies)
}

func (r *recordingJar) record(u *url.URL, cookies []*http.Cookie) {
	if u == nil || u.Host == "" {
		return
	}
	origin := originFromURL(u)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byOrigin[origin] == nil {
		r.byOrigin[origin] = make(map[string]storedCookie)
	}
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		key := cookieKey(cookie)
		if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(time.Now().UTC())) {
			delete(r.byOrigin[origin], key)
			continue
		}
		stored := storedCookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HttpOnly,
		}
		if stored.Path == "" {
			stored.Path = "/"
		}
		if !cookie.Expires.IsZero() {
			stored.ExpiresUnix = cookie.Expires.UTC().Unix()
		}
		r.byOrigin[origin][key] = stored
	}
}

func (r *recordingJar) snapshot(origin string) []storedCookie {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := r.byOrigin[origin]
	if len(entries) == 0 {
		return nil
	}
	out := make([]storedCookie, 0, len(entries))
	for _, cookie := range entries {
		out = append(out, cookie)
	}
	return out
}

func cookieKey(cookie *http.Cookie) string {
	return strings.Join([]string{cookie.Name, cookie.Domain, cookie.Path}, "|")
}
