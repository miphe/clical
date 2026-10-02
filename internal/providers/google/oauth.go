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

	"golang.org/x/oauth2"
	oauth2google "golang.org/x/oauth2/google"

	"github.com/miphe/clical/internal/domain"
)

// ReadOnlyScope is the only calendar access clical asks for.
const ReadOnlyScope = "https://www.googleapis.com/auth/calendar.readonly"

var userInfoURL = "https://openidconnect.googleapis.com/v1/userinfo" // var so tests can point it at a fake

// OAuthConfig is the user's own OAuth client (a "Desktop app" client in their
// Google Cloud project). Google doesn't treat a desktop client's secret as
// confidential the way it treats tokens, but clical keeps it in the vault
// anyway.
type OAuthConfig struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

func (c OAuthConfig) oauth2(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     oauth2google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       []string{ReadOnlyScope, "openid", "email"},
	}
}

// ErrClientRejected means Google refused the OAuth client itself (a wrong
// client secret, or a client ID that doesn't exist) when finishing a
// sign-in. A wrong client ID usually fails earlier, on Google's own page,
// which never redirects back.
var ErrClientRejected = errors.New("google rejected the OAuth client ID or secret")

// SignIn is a browser sign-in in progress: open URL, then Wait. It uses the
// installed-app loopback flow with PKCE: a one-off local web server on an
// ephemeral port receives Google's redirect.
type SignIn struct {
	URL string

	conf     *oauth2.Config
	verifier string
	srv      *http.Server
	listener net.Listener
	codeCh   chan string
	errCh    chan error
}

// BeginSignIn starts a sign-in. loginHint, if set, preselects that account
// on Google's page (used when signing an account in again).
func BeginSignIn(cfg OAuthConfig, loginHint string) (*SignIn, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("google: no OAuth client configured")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("google: listen for the sign-in redirect: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	s := &SignIn{
		conf:     cfg.oauth2(fmt.Sprintf("http://127.0.0.1:%d/callback", port)),
		verifier: oauth2.GenerateVerifier(),
		listener: listener,
		codeCh:   make(chan string, 1),
		errCh:    make(chan error, 1),
	}
	state, err := randomState()
	if err != nil {
		listener.Close()
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "This sign-in link doesn't belong to the running clical.", http.StatusBadRequest)
			s.fail(errors.New("google: sign-in state mismatch"))
		case q.Get("error") != "":
			fmt.Fprintln(w, "Sign-in was cancelled or refused. You can close this tab.")
			s.fail(fmt.Errorf("google: sign-in refused: %s", q.Get("error")))
		case q.Get("code") == "":
			http.Error(w, "Missing authorization code.", http.StatusBadRequest)
			s.fail(errors.New("google: sign-in returned no code"))
		default:
			fmt.Fprintln(w, "Signed in to clical. You can close this tab and return to the terminal.")
			select {
			case s.codeCh <- q.Get("code"):
			default:
			}
		}
	})
	s.srv = &http.Server{Handler: mux}
	go s.srv.Serve(listener)

	opts := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"), oauth2.S256ChallengeOption(s.verifier)}
	if loginHint != "" {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", loginHint))
	}
	s.URL = s.conf.AuthCodeURL(state, opts...)
	return s, nil
}

func (s *SignIn) fail(err error) {
	select {
	case s.errCh <- err:
	default:
	}
}

// Wait blocks until the browser comes back (or ctx ends), then exchanges the
// code for a token and looks up the account's email address.
func (s *SignIn) Wait(ctx context.Context) (domain.Account, *oauth2.Token, error) {
	defer s.Close()
	var code string
	select {
	case code = <-s.codeCh:
	case err := <-s.errCh:
		return domain.Account{}, nil, err
	case <-ctx.Done():
		return domain.Account{}, nil, ctx.Err()
	}

	tok, err := s.conf.Exchange(ctx, code, oauth2.VerifierOption(s.verifier))
	if err != nil {
		var rerr *oauth2.RetrieveError
		if errors.As(err, &rerr) && (rerr.ErrorCode == "invalid_client" || rerr.ErrorCode == "unauthorized_client") {
			return domain.Account{}, nil, fmt.Errorf("%w (%s)", ErrClientRejected, rerr.ErrorCode)
		}
		return domain.Account{}, nil, fmt.Errorf("google: finish sign-in: %w", err)
	}
	email, err := fetchEmail(ctx, s.conf, tok)
	if err != nil {
		return domain.Account{}, nil, fmt.Errorf("google: look up the account: %w", err)
	}
	return domain.Account{ID: domain.NewAccountID("google", email), Provider: "google", Label: email}, tok, nil
}

// Close stops the local redirect server.
func (s *SignIn) Close() {
	s.srv.Close()
}

func fetchEmail(ctx context.Context, conf *oauth2.Config, tok *oauth2.Token) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := conf.Client(ctx, tok).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("userinfo: %s: %s", resp.Status, body)
	}
	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.Email == "" {
		return "", errors.New("userinfo had no email address")
	}
	return info.Email, nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("google: random state: %w", err)
	}
	return hex.EncodeToString(b), nil
}
