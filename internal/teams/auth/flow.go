package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
<html lang="en">
<head>
<meta charset="utf-8" />
<title>Teams Login Helper</title>
<meta name="viewport" content="width=device-width, initial-scale=1" />
<style>
body { font-family: sans-serif; margin: 24px; max-width: 720px; }
label { display: block; font-weight: 600; margin-bottom: 8px; }
textarea { width: 100%; height: 120px; }
#status { margin-top: 12px; }
</style>
</head>
<body>
<h1>Teams Login Helper</h1>
<p>After logging in, open https://teams.live.com/v2 and export localStorage in the browser console:</p>
<pre><code>copy(JSON.stringify(Object.fromEntries(Object.entries(localStorage))))</code></pre>
<p>If <code>copy</code> is unavailable, run the snippet and manually copy the output.</p>
<label for="storage">Paste the localStorage JSON:</label>
<textarea id="storage" placeholder="{&quot;msal.token.keys....&quot;: &quot;{...}&quot;, ...}"></textarea>
<button id="submit">Submit</button>
<div id="status"></div>
<script>
const input = document.getElementById('storage');
const submit = document.getElementById('submit');
const status = document.getElementById('status');
let submitted = false;

function isValidStorage(value) {
  try {
    const parsed = JSON.parse(value.trim());
    return parsed && typeof parsed === 'object';
  } catch (err) {
    return false;
  }
}

function submitURL(value) {
  if (submitted) return;
  submitted = true;
  fetch('/capture', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ storage: value })
  }).then(() => {
    status.textContent = 'Tokens captured. You can close this tab.';
  }).catch(() => {
    status.textContent = 'Failed to submit tokens.';
    submitted = false;
  });
}

input.addEventListener('input', () => {
  if (!isValidStorage(input.value)) {
    status.textContent = '';
    return;
  }
  status.textContent = 'Submitting tokens...';
  submitURL(input.value);
});

submit.addEventListener('click', () => {
  const value = input.value.trim();
  if (!value) {
    status.textContent = 'Paste the localStorage JSON first.';
    return;
  }
  status.textContent = 'Submitting tokens...';
  submitURL(value);
});
</script>
</body>
</html>
`

func WaitForManualState(ctx context.Context, reader io.Reader, writer io.Writer, clientID string) (*AuthState, error) {
	_, _ = fmt.Fprintln(writer, "Paste the localStorage JSON and press Enter:")
	inputCh := make(chan string, 1)
	go func() {
		r := bufio.NewReader(reader)
		line, _ := r.ReadString('\n')
		inputCh <- line
	}()

	select {
	case input := <-inputCh:
		return ExtractTokensFromMSALLocalStorage(input, clientID)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
