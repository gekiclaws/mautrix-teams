package auth

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

var validTenantIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

const (
	defaultAuthorizeEndpoint  = "https://login.live.com/oauth20_authorize.srf"
	defaultTokenEndpoint      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	defaultSkypeTokenEndpoint = "https://teams.live.com/api/auth/v1.0/authz/consumer"
	DefaultClientID           = "4b3e8f46-56d3-427f-b1e2-d239b2ea6bca"
	defaultRedirectURI        = "https://teams.live.com/v2"

	enterpriseSkypeTokenEndpoint = "https://teams.microsoft.com/api/authsvc/v1.0/authz"
	EnterpriseClientID           = "5e3ce6c0-2b1f-4285-8d4b-75ee78787346"
	enterpriseRedirectURI        = "https://teams.microsoft.com"
)

// AccountType distinguishes between consumer (personal) and enterprise (work/school) accounts.
type AccountType string

const (
	AccountTypeConsumer   AccountType = "consumer"
	AccountTypeEnterprise AccountType = "enterprise"
)

var defaultScopes = []string{
	"openid",
	"profile",
	"offline_access",
	"https://graph.microsoft.com/Files.ReadWrite",
}

var enterpriseScopes = []string{
	"openid",
	"profile",
	"offline_access",
	"https://api.spaces.skype.com/.default",
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

func newHTTPClient(store *CookieStore) *http.Client {
	transport := http.DefaultTransport
	if transport == nil {
		transport = &http.Transport{
			ForceAttemptHTTP2:  true,
			DisableCompression: true,
		}
	} else if typed, ok := transport.(*http.Transport); ok {
		transport = typed.Clone()
		transport.(*http.Transport).ForceAttemptHTTP2 = true
		transport.(*http.Transport).DisableCompression = true
	}

	var jar http.CookieJar
	if store != nil {
		jar = store.Jar
	}

	return &http.Client{
		Jar:       jar,
		Transport: &trackingTransport{base: transport, store: store},
		Timeout:   20 * time.Second,
	}
}

func NewClient(store *CookieStore) *Client {
	logger := zerolog.Nop()
	return &Client{
		HTTP:               newHTTPClient(store),
		CookieStore:        store,
		AuthorizeEndpoint:  defaultAuthorizeEndpoint,
		TokenEndpoint:      defaultTokenEndpoint,
		SkypeTokenEndpoint: defaultSkypeTokenEndpoint,
		ClientID:           DefaultClientID,
		RedirectURI:        defaultRedirectURI,
		Scopes:             append([]string(nil), defaultScopes...),
		Log:                &logger,
	}
}

// NewEnterpriseClient returns a Client configured for Enterprise (Work/School) accounts.
// tenantID must be a UUID, or one of "common", "organizations", "consumers".
func NewEnterpriseClient(tenantID string, store *CookieStore) *Client {
	if tenantID == "" {
		tenantID = "common"
	}
	if !validTenantIDPattern.MatchString(tenantID) {
		tenantID = "common"
	}
	logger := zerolog.Nop()
	return &Client{
		HTTP:               newHTTPClient(store),
		CookieStore:        store,
		AuthorizeEndpoint:  "https://login.microsoftonline.com/" + tenantID + "/oauth2/v2.0/authorize",
		TokenEndpoint:      "https://login.microsoftonline.com/" + tenantID + "/oauth2/v2.0/token",
		SkypeTokenEndpoint: enterpriseSkypeTokenEndpoint,
		ClientID:           EnterpriseClientID,
		RedirectURI:        enterpriseRedirectURI,
		Scopes:             append([]string(nil), enterpriseScopes...),
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
