package wloh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the live league site.
const DefaultBaseURL = "https://wordfeud.aasmul.net"

const (
	userAgent        = "Hytte/1.0 (personal league companion)"
	maxResponseBytes = 4 << 20

	fieldSearchBox    = "ctl00$MainContent$TextBoxFinnspiller"
	fieldSearchButton = "ctl00$MainContent$ButtonFinnspiller"
	fieldLoginUser    = "ctl00$MainContent$TextBox1"
	fieldLoginEmail   = "ctl00$MainContent$TextBox2"
	fieldLoginButton  = "ctl00$MainContent$Button1"
	fieldHome         = "ctl00$MainContent$DropDownList2Hjemmespiller"
	fieldAway         = "ctl00$MainContent$DropDownList1Bortespiller"
	fieldHomeScore    = "ctl00$MainContent$TextBox2"
	fieldAwayScore    = "ctl00$MainContent$TextBox4"
	fieldSendButton   = "ctl00$MainContent$butSend"
)

var (
	// ErrPlayerNotFound means the player has no group in the current season.
	ErrPlayerNotFound = errors.New("wloh: player not found in a current group")
	// ErrLoginFailed means the site did not accept the username and email.
	ErrLoginFailed = errors.New("wloh: login failed")
	// ErrNotRegistered means a result was sent but does not show in the group
	// afterwards. The site's own notice, if any, is in ResultError.Notice.
	ErrNotRegistered = errors.New("wloh: result was not registered")
)

// ResultError carries the site's notice when a result post did not stick.
type ResultError struct {
	Notice string
}

func (e *ResultError) Error() string {
	if e.Notice == "" {
		return ErrNotRegistered.Error()
	}
	return ErrNotRegistered.Error() + ": " + e.Notice
}

func (e *ResultError) Unwrap() error { return ErrNotRegistered }

// Client talks to the league site.
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

// NewClient returns a client for the live site.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}, BaseURL: DefaultBaseURL}
}

// session is one browser-like visit: WebForms needs the cookies and the
// hidden state of the previous page to accept a post.
type session struct {
	c  *Client
	hc *http.Client
}

func (c *Client) newSession() *session {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	if c.HTTP != nil {
		hc.Timeout = c.HTTP.Timeout
		hc.Transport = c.HTTP.Transport
	}
	return &session{c: c, hc: hc}
}

func (s *session) do(ctx context.Context, method, path string, form map[string]string) (*page, error) {
	var body io.Reader
	if form != nil {
		vals := url.Values{}
		for k, v := range form {
			vals.Set(k, v)
		}
		body = strings.NewReader(vals.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, s.c.BaseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("wloh: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wloh: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wloh: %s %s returned HTTP %d", method, path, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("wloh: read %s: %w", path, err)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("wloh: %s response too large", path)
	}
	return parsePage(string(data))
}

func (s *session) get(ctx context.Context, path string) (*page, error) {
	return s.do(ctx, http.MethodGet, path, nil)
}

// submit posts p's form to path with extra fields set, as pressing a button would.
func (s *session) submit(ctx context.Context, path string, p *page, extra map[string]string) (*page, error) {
	form := p.formFields()
	for k, v := range extra {
		form[k] = v
	}
	return s.do(ctx, http.MethodPost, path, form)
}

func seriePath(serieID int64) string {
	return "/serie-" + strconv.FormatInt(serieID, 10)
}

func (c *Client) finish(g *Group) *Group {
	g.URL = c.BaseURL + seriePath(g.SerieID)
	return g
}

// GetGroup fetches a group by its id.
func (c *Client) GetGroup(ctx context.Context, serieID int64) (*Group, error) {
	p, err := c.newSession().get(ctx, seriePath(serieID))
	if err != nil {
		return nil, err
	}
	g, err := p.group()
	if err != nil {
		return nil, err
	}
	return c.finish(g), nil
}

// FindGroup looks up the group username plays in this season, using the
// site's player search. Group ids change every season, so this is how a new
// season's group is discovered.
func (c *Client) FindGroup(ctx context.Context, username string) (*Group, error) {
	s := c.newSession()
	front, err := s.get(ctx, "/Default.aspx")
	if err != nil {
		return nil, err
	}
	p, err := s.submit(ctx, "/Default.aspx", front, map[string]string{
		fieldSearchBox:    username,
		fieldSearchButton: "Search",
	})
	if err != nil {
		return nil, err
	}
	g, err := p.group()
	if err != nil {
		return nil, err
	}
	if g.Player(username) == "" {
		return nil, ErrPlayerNotFound
	}
	return c.finish(g), nil
}

func (s *session) login(ctx context.Context, username, email string) error {
	form, err := s.get(ctx, "/Account/Login.aspx")
	if err != nil {
		return err
	}
	p, err := s.submit(ctx, "/Account/Login.aspx", form, map[string]string{
		fieldLoginUser:   username,
		fieldLoginEmail:  email,
		fieldLoginButton: "Log on",
	})
	if err != nil {
		return err
	}
	if p.text(idLoginName) == "" {
		return ErrLoginFailed
	}
	return nil
}

// CheckLogin reports whether the site accepts username and email.
func (c *Client) CheckLogin(ctx context.Context, username, email string) error {
	return c.newSession().login(ctx, username, email)
}

// SubmitResult logs in and registers me's result against opponent in the
// group, then confirms it shows in the group. It returns the group as it is
// after the post.
func (c *Client) SubmitResult(ctx context.Context, username, email string, serieID int64, opponent string, myScore, oppScore int) (*Group, error) {
	s := c.newSession()
	if err := s.login(ctx, username, email); err != nil {
		return nil, err
	}
	path := seriePath(serieID)
	p, err := s.get(ctx, path)
	if err != nil {
		return nil, err
	}
	home, away := "", ""
	for _, o := range p.options(idHomeSelect) {
		if strings.EqualFold(o.label, username) {
			home = o.value
		}
	}
	for _, o := range p.options(idAwaySelect) {
		if strings.EqualFold(o.label, opponent) {
			away = o.value
		}
	}
	if home == "" || away == "" {
		return nil, fmt.Errorf("wloh: result form has no entry for %s vs %s", username, opponent)
	}
	after, err := s.submit(ctx, path, p, map[string]string{
		fieldHome:       home,
		fieldAway:       away,
		fieldHomeScore:  strconv.Itoa(myScore),
		fieldAwayScore:  strconv.Itoa(oppScore),
		fieldSendButton: "Send result",
	})
	if err != nil {
		return nil, err
	}
	notice := strings.TrimSpace(after.text(idFormNotice) + " " + after.text(idPageNotice))
	g, err := after.group()
	if err != nil {
		// The post may still have landed; read the group fresh to find out.
		if g, err = c.GetGroup(ctx, serieID); err != nil {
			return nil, err
		}
	}
	if my, opp, ok := g.Result(username, opponent); !ok || my != myScore || opp != oppScore {
		return c.finish(g), &ResultError{Notice: notice}
	}
	return c.finish(g), nil
}
