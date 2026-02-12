package auth

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const (
	defaultAuthorizeEndpoint  = "https://login.live.com/oauth20_authorize.srf"
	defaultTokenEndpoint      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	defaultSkypeTokenEndpoint = "https://teams.live.com/api/auth/v1.0/authz/consumer"
	defaultClientID           = "4b3e8f46-56d3-427f-b1e2-d239b2ea6bca"
	defaultRedirectURI        = "https://teams.live.com/v2"
)

var defaultScopes = []string{
	"openid",
	"profile",
	"offline_access",
	"https://graph.microsoft.com/Files.ReadWrite",
}

type Client struct {
	HTTP               *http.Client
	CookieStore        *CookieStore
	AuthorizeEndpoint  string
	TokenEndpoint      string
	SkypeTokenEndpoint string
	ClientID           string
	RedirectURI        string
	Scopes             []string
	Log                *zerolog.Logger
}

func NewClient(store *CookieStore) *Client {
	transport := http.DefaultTransport
	if transport == nil {
		transport = &http.Transport{}
	}
	if typed, ok := transport.(*http.Transport); ok {
		typed.ForceAttemptHTTP2 = true
		typed.DisableCompression = true
	}

	var jar http.CookieJar
	if store != nil {
		jar = store.Jar
	}

	httpClient := &http.Client{
		Jar:       jar,
		Transport: &trackingTransport{base: transport, store: store},
		Timeout:   20 * time.Second,
	}
	logger := zerolog.Nop()

	return &Client{
		HTTP:               httpClient,
		CookieStore:        store,
		AuthorizeEndpoint:  defaultAuthorizeEndpoint,
		TokenEndpoint:      defaultTokenEndpoint,
		SkypeTokenEndpoint: defaultSkypeTokenEndpoint,
		ClientID:           defaultClientID,
		RedirectURI:        defaultRedirectURI,
		Scopes:             append([]string(nil), defaultScopes...),
		Log:                &logger,
	}
}

func (c *Client) AttachSkypeToken(req *http.Request, token string) {
	if req == nil || token == "" {
		return
	}
	req.Header.Set("authentication", "skypetoken="+token)
}

func (c *Client) AuthorizeURL(codeChallenge, state string) (string, error) {
	authURL, err := url.Parse(c.AuthorizeEndpoint)
	if err != nil {
		return "", err
	}
	query := authURL.Query()
	query.Set("client_id", c.ClientID)
	query.Set("redirect_uri", c.RedirectURI)
	query.Set("response_type", "code")
	query.Set("response_mode", "fragment")
	query.Set("scope", strings.Join(c.Scopes, " "))
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", "S256")
	if state != "" {
		query.Set("state", state)
	}
	authURL.RawQuery = query.Encode()
	return authURL.String(), nil
}

type trackingTransport struct {
	base  http.RoundTripper
	store *CookieStore
}

func (t *trackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.store != nil {
		t.store.TrackRequest(req)
	}
	return t.base.RoundTrip(req)
}
