package wordfeud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Wordfeud ruleset ids as the app's API numbers them.
const (
	RulesetEnglishUS   = 0
	RulesetNorwegian   = 1
	RulesetDutch       = 2
	RulesetDanish      = 3
	RulesetSwedish     = 4
	RulesetEnglishIntl = 5
)

// BoardNormal is the standard (non-random) board.
const BoardNormal = "normal"

// APIError is an error the Wordfeud API reported in its response envelope,
// e.g. inviting a user that does not exist.
type APIError struct {
	Type    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("wordfeud: %s: %s", e.Type, e.Message)
	}
	return "wordfeud: " + e.Type
}

// flexNumber decodes a JSON number, numeric string or null without ever
// failing, for upstream fields whose exact shape isn't pinned down.
type flexNumber struct {
	value float64
	set   bool
}

func (f *flexNumber) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		f.value, f.set = v, true
	}
	return nil
}

// post sends an authenticated JSON POST and returns the envelope's content.
func (c *Client) post(sessionToken, path string, payload any) (json.RawMessage, error) {
	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return nil, fmt.Errorf("wordfeud: marshal %s request: %w", path, err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("wordfeud: create %s request: %w", path, err)
	}
	c.setAuth(req, sessionToken)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: %s request failed: %w", path, err)
	}
	defer resp.Body.Close()

	if err := checkRedirect(resp); err != nil {
		return nil, err
	}
	data, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wordfeud: read %s response: %w", path, err)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("wordfeud: %s: %w", path, ErrSessionExpired)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wordfeud: %s returned HTTP %d: %s", path, resp.StatusCode, data)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf("wordfeud: parse %s response: %w", path, err)
	}
	if apiResp.Status != "success" {
		var e struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(apiResp.Content, &e)
		if e.Type == "" || e.Type == "login_required" {
			return nil, fmt.Errorf("wordfeud: %s failed (status=%s): %w", path, apiResp.Status, ErrSessionExpired)
		}
		return nil, &APIError{Type: e.Type, Message: e.Message}
	}
	return apiResp.Content, nil
}

// Invites lists the usernames with a pending invitation in each direction.
type Invites struct {
	Sent     []string // players the user has invited
	Received []string // players who have invited the user
}

// inviteName reads a username from an invitation entry. The counterpart is
// either a plain username or a user object, depending on API version.
func inviteName(entry map[string]json.RawMessage, key string) string {
	raw, ok := entry[key]
	if !ok {
		return ""
	}
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return name
	}
	var user struct {
		Username string `json:"username"`
	}
	if json.Unmarshal(raw, &user) == nil {
		return user.Username
	}
	return ""
}

// GetInvites fetches the user's pending invitations.
func (c *Client) GetInvites(sessionToken string) (*Invites, error) {
	content, err := c.post(sessionToken, "/user/status/", nil)
	if err != nil {
		return nil, err
	}
	var status struct {
		Sent     []map[string]json.RawMessage `json:"invites_sent"`
		Received []map[string]json.RawMessage `json:"invites_received"`
	}
	if err := json.Unmarshal(content, &status); err != nil {
		return nil, fmt.Errorf("wordfeud: parse status content: %w", err)
	}
	inv := &Invites{}
	for _, e := range status.Sent {
		if name := inviteName(e, "invitee"); name != "" {
			inv.Sent = append(inv.Sent, name)
		}
	}
	for _, e := range status.Received {
		if name := inviteName(e, "inviter"); name != "" {
			inv.Received = append(inv.Received, name)
		}
	}
	return inv, nil
}

// Invite challenges username to a new game.
func (c *Client) Invite(sessionToken, username string, ruleset int, boardType string) error {
	_, err := c.post(sessionToken, "/invite/new/", map[string]any{
		"invitee":    username,
		"ruleset":    ruleset,
		"board_type": boardType,
	})
	return err
}
