package auth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/rs/zerolog"
)

type HelperListener struct {
	URL      string
	server   *http.Server
	ln       net.Listener
	stateCh  chan *AuthState
	once     sync.Once
	clientID string
}

func StartHelperListener(ctx context.Context, log *zerolog.Logger, clientID string) (*HelperListener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	listener := &HelperListener{
		URL:      "http://" + ln.Addr().String() + "/",
		ln:       ln,
		stateCh:  make(chan *AuthState, 1),
		clientID: clientID,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", listener.handleIndex)
	mux.HandleFunc("/capture", listener.handleCapture(log))

	listener.server = &http.Server{
		Handler: mux,
	}

	go func() {
		_ = listener.server.Serve(ln)
	}()

	go func() {
		<-ctx.Done()
		_ = listener.server.Shutdown(context.Background())
	}()

	return listener, nil
}

func (h *HelperListener) WaitForState(ctx context.Context) (*AuthState, error) {
	select {
	case state := <-h.stateCh:
		_ = h.server.Shutdown(context.Background())
		return state, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *HelperListener) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, helperPageHTML)
}

func (h *HelperListener) handleCapture(log *zerolog.Logger) http.HandlerFunc {
	logger := zerolog.Nop()
	if log != nil {
		logger = *log
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var payload struct {
			Storage string `json:"storage"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Storage == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		state, err := ExtractTokensFromMSALLocalStorage(payload.Storage, h.clientID)
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to extract tokens from localStorage")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		h.once.Do(func() {
			h.stateCh <- state
		})

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}
}

const helperPageHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<title>Teams Login Helper</title>
</head>
<body>
<p>Completing login...</p>
<script>
async function capture() {
  try {
    const storage = JSON.stringify(
      Object.fromEntries(Object.entries(localStorage))
    );

    await fetch('/capture', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ storage })
    });

    document.body.innerHTML = "<p>Login complete. You may close this tab.</p>";
  } catch (err) {
    document.body.innerHTML = "<p>Failed to capture tokens.</p>";
  }
}

capture();
</script>
</body>
</html>
`
