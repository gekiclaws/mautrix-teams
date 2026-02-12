package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	teamsclient "go.mau.fi/mautrix-teams/internal/teams/client"
)

const (
	defaultUploadEndpoint = "https://graph.microsoft.com/v1.0/me/drive/root:/Microsoft Teams Chat Files"
	maxUploadErrorBytes   = 1024
	defaultMaxUploadBytes = 100 * 1024 * 1024

	// DefaultSmallFileLimit is a conservative limit for the simple PUT upload flow.
	// Larger payloads should use createUploadSession + chunked upload.
	DefaultSmallFileLimit = 4 * 1024 * 1024

	// DefaultChunkSize is Graph-safe (and a multiple of 320 KiB).
	DefaultChunkSize = 5 * 1024 * 1024
	chunkSizeAlign   = 320 * 1024
)

var (
	ErrMissingGraphHTTPClient   = errors.New("graph client missing http client")
	ErrMissingGraphAccessToken  = errors.New("missing graph access token")
	ErrEmptyUploadFilename      = errors.New("upload filename is empty")
	ErrEmptyUploadContent       = errors.New("upload content is empty")
	ErrUploadContentTooLarge    = errors.New("upload content exceeds max size")
	ErrMissingListItemUniqueID  = errors.New("upload response missing sharepointIds.listItemUniqueId")
	ErrMissingUploadDriveItemID = errors.New("upload response missing id")
	ErrMissingUploadSiteURL     = errors.New("upload response missing parentReference.sharepointIds.siteUrl")
)

type UploadedDriveItem struct {
	DriveItemID      string
	ListItemUniqueID string
	SiteURL          string
	FileName         string
	Size             int64
}

type GraphUploadError struct {
	Status      int
	BodySnippet string
}

func (e GraphUploadError) Error() string {
	return "graph upload request failed"
}

type GraphClient struct {
	HTTP           *http.Client
	Executor       *teamsclient.TeamsRequestExecutor
	Log            *zerolog.Logger
	AccessToken    string
	UploadBaseURL  string
	MaxUploadSize  int
	SmallFileLimit int
	ChunkSize      int
}

func NewClient(httpClient *http.Client) *GraphClient {
	executor := &teamsclient.TeamsRequestExecutor{
		HTTP:        httpClient,
		Log:         zerolog.Nop(),
		MaxRetries:  4,
		BaseBackoff: 500 * time.Millisecond,
		MaxBackoff:  10 * time.Second,
	}
	return &GraphClient{
		HTTP:           httpClient,
		Executor:       executor,
		UploadBaseURL:  defaultUploadEndpoint,
		MaxUploadSize:  defaultMaxUploadBytes,
		SmallFileLimit: DefaultSmallFileLimit,
		ChunkSize:      DefaultChunkSize,
	}
}

func (c *GraphClient) UploadTeamsChatFile(ctx context.Context, filename string, content []byte) (*UploadedDriveItem, error) {
	if c == nil || c.HTTP == nil {
		return nil, ErrMissingGraphHTTPClient
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return nil, ErrMissingGraphAccessToken
	}
	if strings.TrimSpace(filename) == "" {
		return nil, ErrEmptyUploadFilename
	}
	if len(content) == 0 {
		return nil, ErrEmptyUploadContent
	}
	maxUploadSize := c.MaxUploadSize
	if maxUploadSize <= 0 {
		maxUploadSize = defaultMaxUploadBytes
	}
	if len(content) > maxUploadSize {
		return nil, ErrUploadContentTooLarge
	}

	smallLimit := c.SmallFileLimit
	if smallLimit <= 0 {
		smallLimit = DefaultSmallFileLimit
	}
	if len(content) > smallLimit {
		uploadURL, err := c.CreateUploadSession(ctx, filename)
		if err != nil {
			return nil, err
		}
		return c.UploadFileInChunks(ctx, uploadURL, content)
	}

	endpoint, err := c.uploadURL(filename)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.AccessToken))
	req.Header.Set("Content-Type", "application/octet-stream")

	executor := c.executor()

	resp, err := executor.Do(ctx, req, classifyUploadResponse)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	defer resp.Body.Close()
	return parseUploadedDriveItem(resp.Body)
}

func parseUploadedDriveItem(r io.Reader) (*UploadedDriveItem, error) {
	var payload struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Size          int64  `json:"size"`
		SharepointIDs struct {
			ListItemUniqueID string `json:"listItemUniqueId"`
		} `json:"sharepointIds"`
		ParentReference struct {
			SharepointIDs struct {
				SiteURL string `json:"siteUrl"`
			} `json:"sharepointIds"`
		} `json:"parentReference"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.ID) == "" {
		return nil, ErrMissingUploadDriveItemID
	}
	if strings.TrimSpace(payload.SharepointIDs.ListItemUniqueID) == "" {
		return nil, ErrMissingListItemUniqueID
	}
	if strings.TrimSpace(payload.ParentReference.SharepointIDs.SiteURL) == "" {
		return nil, ErrMissingUploadSiteURL
	}
	return &UploadedDriveItem{
		DriveItemID:      strings.TrimSpace(payload.ID),
		ListItemUniqueID: strings.TrimSpace(payload.SharepointIDs.ListItemUniqueID),
		SiteURL:          strings.TrimSpace(payload.ParentReference.SharepointIDs.SiteURL),
		FileName:         strings.TrimSpace(payload.Name),
		Size:             payload.Size,
	}, nil
}

func (c *GraphClient) uploadURL(filename string) (string, error) {
	base := strings.TrimSpace(c.UploadBaseURL)
	if base == "" {
		base = defaultUploadEndpoint
	}
	escapedName := url.PathEscape(strings.TrimSpace(filename))
	endpoint := strings.TrimRight(base, "/") + "/" + escapedName + ":/content"
	q := url.Values{}
	q.Set("@microsoft.graph.conflictBehavior", "rename")
	q.Set("$select", "*,sharepointIds")
	return endpoint + "?" + q.Encode(), nil
}

func classifyUploadResponse(resp *http.Response) error {
	if resp == nil {
		return errors.New("missing response")
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return teamsclient.RetryableError{
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return teamsclient.RetryableError{Status: resp.StatusCode}
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxUploadErrorBytes))
	return GraphUploadError{
		Status:      resp.StatusCode,
		BodySnippet: string(snippet),
	}
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if duration := time.Until(at); duration > 0 {
			return duration
		}
	}
	return 0
}

func (c *GraphClient) executor() *teamsclient.TeamsRequestExecutor {
	executor := c.Executor
	if executor == nil {
		executor = &teamsclient.TeamsRequestExecutor{
			HTTP:        c.HTTP,
			Log:         zerolog.Nop(),
			MaxRetries:  4,
			BaseBackoff: 500 * time.Millisecond,
			MaxBackoff:  10 * time.Second,
		}
		c.Executor = executor
	}
	if executor.HTTP == nil {
		executor.HTTP = c.HTTP
	}
	if c.Log != nil {
		executor.Log = *c.Log
	}
	return executor
}
