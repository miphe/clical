package google

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"

	"golang.org/x/oauth2"

	"github.com/miphe/clical/internal/domain"
)

const userInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"

// Authenticate runs the installed-app / loopback OAuth2 flow: it starts a
// local HTTP server on an ephemeral port, presents the consent URL to the
// user (via promptURL/openBrowser), waits for Google's redirect carrying the
// authorization code, exchanges it for a token, and persists that token to
// the vault under the resulting account's ID. The token itself is never
// returned — CalendarProvider callers only ever see the Account.
func (p *Provider) Authenticate(ctx context.Context) (domain.Account, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return domain.Account{}, fmt.Errorf("google: listen for oauth redirect: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	conf := p.oauth2Config(redirectURL)

	state, err := randomState()
	if err != nil {
		return domain.Account{}, fmt.Errorf("google: generate state: %w", err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "invalid state", http.StatusBadRequest)
			errCh <- errors.New("google: oauth callback: state mismatch")
		case q.Get("error") != "":
			http.Error(w, "authorization denied", http.StatusBadRequest)
			errCh <- fmt.Errorf("google: oauth callback: %s", q.Get("error"))
		case q.Get("code") == "":
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- errors.New("google: oauth callback: missing code")
		default:
			fmt.Fprintln(w, "Authenticated with clical — you can close this tab.")
			codeCh <- q.Get("code")
		}
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	defer srv.Shutdown(context.Background())

	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	if p.promptURL != nil {
		p.promptURL(authURL)
	}
	if p.openBrowser != nil {
		_ = p.openBrowser(authURL)
	}

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return domain.Account{}, err
	case <-ctx.Done():
		return domain.Account{}, ctx.Err()
	}

	tok, err := conf.Exchange(ctx, code)
	if err != nil {
		return domain.Account{}, fmt.Errorf("google: exchange code: %w", err)
	}

	email, err := fetchEmail(ctx, conf, tok)
	if err != nil {
		return domain.Account{}, fmt.Errorf("google: fetch account email: %w", err)
	}

	acct := domain.Account{
		ID:       domain.NewAccountID("google", email),
		Provider: "google",
		Label:    email,
	}

	if err := p.vault.SaveToken(acct.ID, tok); err != nil {
		return domain.Account{}, fmt.Errorf("google: save token: %w", err)
	}

	return acct, nil
}

func fetchEmail(ctx context.Context, conf *oauth2.Config, tok *oauth2.Token) (string, error) {
	client := conf.Client(ctx, tok)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("userinfo request failed: %s: %s", resp.Status, body)
	}

	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.Email == "" {
		return "", errors.New("userinfo response had no email")
	}
	return info.Email, nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// defaultOpenBrowser fires the platform's "open a URL" command and does not
// wait for it — it's just a convenience on top of promptURL, whose failure
// is ignored by the caller.
func defaultOpenBrowser(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Start()
}
