package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/Robin831/Hytte/internal/training"
	"golang.org/x/net/html"
)

// Import errors. Callers match them with errors.Is; the HTTP handler maps each
// one onto a status code. They are deliberately distinct so a page that could
// not be fetched (upstream problem) is not reported the same way as a page
// Claude could not turn into a recipe (content problem).
var (
	// ErrInvalidURL means the supplied string is not an absolute http(s) URL.
	ErrInvalidURL = errors.New("recipe import: invalid url")
	// ErrFetch means the page could not be retrieved: DNS/connection failure,
	// a blocked address, a non-2xx status or a read error.
	ErrFetch = errors.New("recipe import: could not fetch page")
	// ErrClaude means the Claude CLI call itself failed (disabled, missing
	// binary, timeout).
	ErrClaude = errors.New("recipe import: claude call failed")
	// ErrParse means Claude answered but the answer was not a usable recipe.
	ErrParse = errors.New("recipe import: could not parse recipe from page")
)

const (
	// fetchTimeout bounds the whole page fetch, including connect and body read.
	fetchTimeout = 15 * time.Second
	// maxPageBytes caps how much of the response body is read. Recipe pages are
	// far smaller than this; anything larger is truncated rather than rejected,
	// since the recipe is almost always in the first chunk of markup.
	maxPageBytes = 2 << 20 // 2 MiB
	// maxPageRunes caps the extracted text handed to Claude so a bloated page
	// cannot blow the prompt budget.
	maxPageRunes = 30000
	// maxImportBodyBytes caps the request body of POST /api/recipes/import.
	maxImportBodyBytes = 8 << 10 // 8 KiB
	// importUserAgent identifies the fetcher to the sites we read.
	importUserAgent = "Hytte-RecipeImport/1.0 (+https://robinedvardsmith.com)"
)

// dialControl is the SSRF guard applied when the fetcher opens a connection.
// It runs after DNS resolution on the address actually dialled, so a hostname
// that resolves to a private address is blocked even if the name looks public.
// Tests set it to nil so they can point the fetcher at an httptest server on
// 127.0.0.1.
var dialControl = blockPrivateAddress

// importHTTPClient is the shared client for page fetches. It is a package-level
// singleton so connections are pooled across imports rather than a fresh client
// (and transport) being built per request.
var importHTTPClient = &http.Client{
	Timeout: fetchTimeout,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			Control: func(network, address string, c syscall.RawConn) error {
				if dialControl == nil {
					return nil
				}
				return dialControl(network, address, c)
			},
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// runPrompt is the Claude CLI seam, mirroring the one in grocery/translate.go.
// Tests override it to return a scripted response instead of spawning the CLI.
var runPrompt = training.RunPrompt

// ParsedRecipe is the result of importing a recipe from a URL. It is a
// review-and-edit payload, not a stored entity: it carries no IDs, no owner and
// no timestamps, and nothing writes it to the database. The user edits it in
// the client and submits it through POST /api/recipes when they are happy.
type ParsedRecipe struct {
	Title       string             `json:"title"`
	Notes       string             `json:"notes"`
	Servings    int                `json:"servings"`
	Ingredients []ParsedIngredient `json:"ingredients"`
	Steps       []ParsedStep       `json:"steps"`
	// SourceURL is the page the recipe came from. It is filled in by the
	// importer, never by Claude.
	SourceURL string `json:"source_url"`
}

// ParsedIngredient is one extracted ingredient line. Text is the raw line as it
// appeared on the page; Quantity, Unit and Name are the parsed triple, matching
// the split the stored Ingredient uses.
type ParsedIngredient struct {
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Name     string  `json:"name"`
	Text     string  `json:"text"`
}

// ParsedStep is one extracted method step. DurationSeconds is 0 when the step
// declares no time.
type ParsedStep struct {
	Text            string `json:"text"`
	DurationSeconds int    `json:"duration_seconds"`
}

// ImportFromURL fetches rawURL, reduces the page to text and asks Claude to
// turn it into a structured recipe. It performs no database writes at all, so a
// failure at any stage leaves nothing behind.
//
// cfg is the caller's Claude configuration, loaded the same way the grocery
// translation endpoint loads it — the importer reuses that shared client rather
// than owning its own model or credentials.
func ImportFromURL(ctx context.Context, cfg *training.ClaudeConfig, rawURL string) (ParsedRecipe, error) {
	normalized, err := NormalizeImportURL(rawURL)
	if err != nil {
		return ParsedRecipe{}, err
	}

	page, err := fetchPage(ctx, normalized)
	if err != nil {
		return ParsedRecipe{}, err
	}

	text := htmlToText(page)
	if text == "" {
		return ParsedRecipe{}, fmt.Errorf("%w: page contained no readable text", ErrParse)
	}

	output, err := runPrompt(ctx, cfg, buildImportPrompt(normalized, text))
	if err != nil {
		return ParsedRecipe{}, fmt.Errorf("%w: %v", ErrClaude, err)
	}

	parsed, err := parseRecipeJSON(output)
	if err != nil {
		return ParsedRecipe{}, err
	}
	parsed.SourceURL = normalized
	return parsed, nil
}

// NormalizeImportURL validates that rawURL is an absolute http(s) URL with a
// host and returns it in canonical form. Anything else — a relative path, a
// file:// or javascript: URL, a bare hostname — is rejected.
func NormalizeImportURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("%w: url is required", ErrInvalidURL)
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: url has no host", ErrInvalidURL)
	}
	return u.String(), nil
}

// blockPrivateAddress refuses connections to loopback, private, link-local and
// unspecified addresses. Because net.Dialer.Control runs on the resolved
// address rather than the hostname, this also stops a public name that resolves
// to an internal IP (DNS rebinding) from reaching the local network or a cloud
// metadata endpoint.
func blockPrivateAddress(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: unparseable dial address %q", ErrFetch, address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: dial address %q is not an IP", ErrFetch, address)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("%w: refusing to fetch internal address %s", ErrFetch, ip)
	}
	return nil
}

// fetchPage retrieves rawURL and returns the response body as a string. The
// body is capped at maxPageBytes; when the page is larger it is truncated and
// the truncation is logged rather than passing silently.
func fetchPage(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrFetch, err)
	}
	req.Header.Set("User-Agent", importUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("Accept-Language", "nb,no;q=0.9,en;q=0.8")

	resp, err := importHTTPClient.Do(req)
	if err != nil {
		// A blocked address surfaces as a dial error wrapping ErrFetch already;
		// wrapping again is harmless and keeps every failure path uniform.
		return "", fmt.Errorf("%w: %v", ErrFetch, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: %s responded %d", ErrFetch, rawURL, resp.StatusCode)
	}

	// Read one byte past the cap so an oversized page is detectable rather than
	// silently truncated.
	limited := &io.LimitedReader{R: resp.Body, N: maxPageBytes + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("%w: reading body: %v", ErrFetch, err)
	}
	if len(body) > maxPageBytes {
		log.Printf("recipes: import: %s exceeded %d bytes, truncating", rawURL, maxPageBytes)
		body = body[:maxPageBytes]
	}
	return string(body), nil
}

// skippedElements are the elements whose subtree carries no recipe content.
// Dropping them keeps the prompt focused on the article body.
var skippedElements = map[string]bool{
	"style":    true,
	"noscript": true,
	"nav":      true,
	"footer":   true,
	"aside":    true,
	"form":     true,
	"svg":      true,
	"iframe":   true,
	"template": true,
}

// htmlToText reduces a page to the plain text Claude reads: one collapsed line
// per text node, in document order.
//
// <script> subtrees are skipped with one exception: schema.org JSON-LD blocks
// are kept verbatim. Recipe sites publish the full structured recipe there, and
// it parses far more reliably than the surrounding prose.
func htmlToText(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		// html.Parse only fails on reader errors, which strings.Reader cannot
		// produce; fall back to the raw markup so a future change here cannot
		// silently drop the page.
		return truncateRunes(collapseLine(raw), maxPageRunes)
	}

	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.ElementNode:
			if n.Data == "script" {
				if strings.Contains(strings.ToLower(scriptType(n)), "ld+json") {
					writeLine(&b, nodeText(n))
				}
				return
			}
			if skippedElements[n.Data] {
				return
			}
		case html.TextNode:
			writeLine(&b, n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return truncateRunes(strings.TrimSpace(b.String()), maxPageRunes)
}

// scriptType returns the type attribute of a <script> element, or "".
func scriptType(n *html.Node) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, "type") {
			return attr.Val
		}
	}
	return ""
}

// nodeText concatenates every text node beneath n.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// writeLine appends s as a single whitespace-collapsed line, skipping content
// that is whitespace only.
func writeLine(b *strings.Builder, s string) {
	line := collapseLine(s)
	if line == "" {
		return
	}
	b.WriteString(line)
	b.WriteByte('\n')
}

// collapseLine squeezes every run of whitespace in s down to a single space.
func collapseLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncateRunes cuts s to at most max runes, never splitting a rune.
func truncateRunes(s string, max int) string {
	if len(s) <= max { // byte length bounds rune count, so this is a safe fast path
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// buildImportPrompt renders the extraction prompt. It asks for bare JSON and is
// paired with stripCodeFence below, which cleans up the fences Claude sometimes
// adds anyway.
func buildImportPrompt(sourceURL, pageText string) string {
	return fmt.Sprintf(`Extract the recipe from the web page text below and return it as JSON.

Return ONLY a single JSON object — no prose, no explanation, no markdown code fences. Use exactly this shape:

{
  "title": "recipe name",
  "notes": "short description, source attribution or serving tips; empty string if none",
  "servings": 4,
  "ingredients": [
    {"quantity": 400, "unit": "g", "name": "torsk", "text": "400 g torsk, i terninger"}
  ],
  "steps": [
    {"text": "Kok makaronien.", "duration_seconds": 480}
  ]
}

Rules:
- Keep the language of the original page; do not translate.
- "text" is the ingredient line exactly as written on the page.
- "quantity" is a number (use 0 when the line states no amount), "unit" and "name" are the parsed parts of that line (use "" when absent).
- "servings" is a whole number (use 0 when the page does not state a yield).
- "duration_seconds" is the time that step takes in seconds (use 0 when the step states no time).
- Keep the steps in the order they appear on the page and do not merge them.
- If the page contains no recipe, return {"title": "", "ingredients": [], "steps": []}.

Source URL: %s

Page text:
%s`, sourceURL, pageText)
}

// parseRecipeJSON turns Claude's answer into a validated ParsedRecipe. Every
// failure — malformed JSON, or JSON that decodes but describes no usable recipe
// — is reported as ErrParse so callers need to match only one error.
func parseRecipeJSON(output string) (ParsedRecipe, error) {
	cleaned := stripCodeFence(output)
	if cleaned == "" {
		return ParsedRecipe{}, fmt.Errorf("%w: claude returned an empty response", ErrParse)
	}

	var parsed ParsedRecipe
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return ParsedRecipe{}, fmt.Errorf("%w: %v (response length %d)", ErrParse, err, len(cleaned))
	}

	parsed.Title = strings.TrimSpace(parsed.Title)
	parsed.Notes = strings.TrimSpace(parsed.Notes)
	if parsed.Servings < 0 {
		parsed.Servings = 0
	}
	// SourceURL is set by the importer; discard anything Claude invented.
	parsed.SourceURL = ""

	ingredients := make([]ParsedIngredient, 0, len(parsed.Ingredients))
	for _, in := range parsed.Ingredients {
		in.Text = strings.TrimSpace(in.Text)
		in.Unit = strings.TrimSpace(in.Unit)
		in.Name = strings.TrimSpace(in.Name)
		if in.Text == "" && in.Name == "" {
			continue
		}
		if in.Quantity < 0 {
			in.Quantity = 0
		}
		// Fall back to the parsed name when the model omitted the raw line, so
		// the client always has something to render.
		if in.Text == "" {
			in.Text = in.Name
		}
		ingredients = append(ingredients, in)
	}
	parsed.Ingredients = ingredients

	steps := make([]ParsedStep, 0, len(parsed.Steps))
	for _, st := range parsed.Steps {
		st.Text = strings.TrimSpace(st.Text)
		if st.Text == "" {
			continue
		}
		if st.DurationSeconds < 0 {
			st.DurationSeconds = 0
		}
		steps = append(steps, st)
	}
	parsed.Steps = steps

	if parsed.Title == "" {
		return ParsedRecipe{}, fmt.Errorf("%w: no recipe title found on the page", ErrParse)
	}
	if len(parsed.Ingredients) == 0 {
		return ParsedRecipe{}, fmt.Errorf("%w: no ingredients found on the page", ErrParse)
	}
	if len(parsed.Steps) == 0 {
		return ParsedRecipe{}, fmt.Errorf("%w: no steps found on the page", ErrParse)
	}

	return parsed, nil
}

// stripCodeFence removes a surrounding ```json … ``` fence and any prose around
// the JSON object. It is defensive: the prompt asks for bare JSON, but models
// occasionally wrap it anyway.
func stripCodeFence(output string) string {
	out := strings.TrimSpace(output)
	if strings.HasPrefix(out, "```") {
		if nl := strings.IndexByte(out, '\n'); nl >= 0 {
			out = out[nl+1:]
		} else {
			out = strings.TrimPrefix(out, "```")
		}
		if end := strings.LastIndex(out, "```"); end >= 0 {
			out = out[:end]
		}
		out = strings.TrimSpace(out)
	}
	// Trim any leading/trailing commentary around the object itself. Only do so
	// when both braces are present and correctly ordered, so genuinely
	// non-JSON output ("not json") still fails to parse rather than being
	// mangled into something that might.
	start := strings.IndexByte(out, '{')
	end := strings.LastIndexByte(out, '}')
	if start >= 0 && end > start {
		out = out[start : end+1]
	}
	return strings.TrimSpace(out)
}
