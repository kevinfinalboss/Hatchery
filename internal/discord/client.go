package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls Discord's REST API as the application.
type Client struct {
	AppID        string
	BotToken     string
	ClientSecret string
	// BaseURL defaults to Discord's API; tests point it at a fake.
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultAPIBase
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// do sends a request and decodes a JSON answer into out (when not nil). auth is the whole
// Authorization header ("" for webhook calls, which authenticate by token in the path).
func (c *Client) do(ctx context.Context, method, path, auth string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, body)
	if err != nil {
		return err
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", "DiscordBot (https://github.com/kevinfinalboss/Hatchery, 1)")
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("discord %s: %w", path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("discord %s %s: HTTP %d: %s", method, redactPath(path), res.StatusCode, truncateBody(raw))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// redactPath hides interaction tokens (webhook paths) in errors.
func redactPath(path string) string {
	if strings.HasPrefix(path, "/webhooks/") {
		return "/webhooks/…"
	}
	return path
}

func truncateBody(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}

func (c *Client) botJSON(ctx context.Context, method, path string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, method, path, "Bot "+c.BotToken, bytes.NewReader(raw), "application/json", out)
}

// RegisterGlobalCommands replaces the application's global commands.
func (c *Client) RegisterGlobalCommands(ctx context.Context, cmds []Command) error {
	return c.botJSON(ctx, http.MethodPut, "/applications/"+c.AppID+"/commands", cmds, nil)
}

// RegisterGuildCommands replaces the application's commands in one guild.
func (c *Client) RegisterGuildCommands(ctx context.Context, guildID string, cmds []Command) error {
	return c.botJSON(ctx, http.MethodPut, "/applications/"+c.AppID+"/guilds/"+guildID+"/commands", cmds, nil)
}

// EditOriginal completes a deferred interaction response. It authenticates by the interaction
// token, not the bot token.
func (c *Client) EditOriginal(ctx context.Context, interactionToken, content string) error {
	raw, _ := json.Marshal(map[string]any{"content": Truncate(content)})
	return c.do(ctx, http.MethodPatch, "/webhooks/"+c.AppID+"/"+interactionToken+"/messages/@original", "",
		bytes.NewReader(raw), "application/json", nil)
}

// Guild is the guild an application was just installed in (from the OAuth2 token response).
type Guild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Token is an OAuth2 token response. It is used once and never stored.
type Token struct {
	AccessToken string `json:"access_token"`
	Guild       *Guild `json:"guild"`
}

// ExchangeCode trades an OAuth2 authorization code for a token.
func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI string) (Token, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.SetBasicAuth(c.AppID, c.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http().Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("discord token exchange: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return Token{}, fmt.Errorf("discord token exchange: HTTP %d", res.StatusCode)
	}
	var tok Token
	if err := json.Unmarshal(raw, &tok); err != nil {
		return Token{}, err
	}
	return tok, nil
}

// Me returns the user an OAuth2 access token belongs to.
func (c *Client) Me(ctx context.Context, accessToken string) (User, error) {
	var u User
	err := c.do(ctx, http.MethodGet, "/users/@me", "Bearer "+accessToken, nil, "", &u)
	return u, err
}

// GatewayURL returns the WebSocket URL the bot connects to.
func (c *Client) GatewayURL(ctx context.Context) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodGet, "/gateway/bot", "Bot "+c.BotToken, nil, "", &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// AuthorizeURL is the page that asks the user to authorize the application. permissions is only
// sent for bot installs ("" otherwise).
func (c *Client) AuthorizeURL(scopes []string, state, redirectURI, permissions string) string {
	q := url.Values{
		"client_id":     {c.AppID},
		"response_type": {"code"},
		"scope":         {strings.Join(scopes, " ")},
		"state":         {state},
		"redirect_uri":  {redirectURI},
		"prompt":        {"consent"},
	}
	if permissions != "" {
		q.Set("permissions", permissions)
		q.Set("integration_type", "0")
	}
	return defaultAuthorizeURL + "?" + q.Encode()
}
