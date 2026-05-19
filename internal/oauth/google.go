package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"mechhub-back/internal/config"
)

const userInfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"

type Google struct {
	cfg *oauth2.Config
}

type GoogleUser struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func NewGoogle(cfg config.GoogleConfig, backendBaseURL string) *Google {
	return &Google{
		cfg: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  backendBaseURL + "/api/auth/google/callback",
			Endpoint:     google.Endpoint,
			Scopes:       []string{"openid", "email", "profile"},
		},
	}
}

func (g *Google) AuthURL(state string) string {
	return g.cfg.AuthCodeURL(state, oauth2.AccessTypeOnline, oauth2.SetAuthURLParam("prompt", "select_account"))
}

func (g *Google) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return g.cfg.Exchange(ctx, code)
}

func (g *Google) FetchUser(ctx context.Context, tok *oauth2.Token) (*GoogleUser, error) {
	client := g.cfg.Client(ctx, tok)
	req, err := http.NewRequestWithContext(ctx, "GET", userInfoURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("google userinfo: status " + resp.Status)
	}
	var u GoogleUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

type PictureBlob struct {
	Body        io.ReadCloser
	ContentType string
}

func (g *Google) DownloadPicture(ctx context.Context, url string) (*PictureBlob, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, errors.New("google picture: status " + resp.Status)
	}
	return &PictureBlob{Body: resp.Body, ContentType: resp.Header.Get("Content-Type")}, nil
}
