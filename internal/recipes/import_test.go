package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

// recipePageHTML is a realistic-enough page: the recipe sits inside an article
// surrounded by navigation and script noise that htmlToText must drop.
const recipePageHTML = `<!doctype html>
<html lang="nb">
<head>
  <title>Fiskegrateng | Oppskrift</title>
  <style>body { color: red; }</style>
  <script>window.tracking = {id: "should-not-appear"};</script>
</head>
<body>
  <nav><a href="/">Forsiden</a><a href="/annonse">Reklame som ikke hoerer hjemme</a></nav>
  <article>
    <h1>Fiskegrateng</h1>
    <p>4 porsjoner</p>
    <ul>
      <li>400 g torsk, i terninger</li>
      <li>3 dl melk</li>
    </ul>
    <ol>
      <li>Kok makaronien i 8 minutter.</li>
      <li>Stek i ovnen.</li>
    </ol>
  </article>
  <footer>Kontakt oss</footer>
</body>
</html>`

// claudeRecipeJSON is a well-formed model response for recipePageHTML,
// including one step with a duration and one without.
const claudeRecipeJSON = `{
  "title": "Fiskegrateng",
  "notes": "Klassisk norsk hverdagsmiddag.",
  "servings": 4,
  "ingredients": [
    {"quantity": 400, "unit": "g", "name": "torsk", "text": "400 g torsk, i terninger"},
    {"quantity": 3, "unit": "dl", "name": "melk", "text": "3 dl melk"}
  ],
  "steps": [
    {"text": "Kok makaronien i 8 minutter.", "duration_seconds": 480},
    {"text": "Stek i ovnen.", "duration_seconds": 0}
  ]
}`

// stubClaude records the prompt it was handed and replays a scripted answer.
type stubClaude struct {
	resp   string
	err    error
	calls  int
	prompt string
}

func (s *stubClaude) run(_ context.Context, _ *training.ClaudeConfig, prompt string) (string, error) {
	s.calls++
	s.prompt = prompt
	if s.err != nil {
		return "", s.err
	}
	return s.resp, nil
}

// installStubClaude swaps the Claude seam for the duration of the test.
func installStubClaude(t *testing.T, stub *stubClaude) {
	t.Helper()
	previous := runPrompt
	runPrompt = stub.run
	t.Cleanup(func() { runPrompt = previous })
}

// allowLocalFetches disables the SSRF dial guard so the importer can reach an
// httptest server on 127.0.0.1. Production keeps the guard.
func allowLocalFetches(t *testing.T) {
	t.Helper()
	previous := dialControl
	dialControl = nil
	t.Cleanup(func() { dialControl = previous })
}

// enabledConfig is the Claude config the importer is handed; the stub ignores
// it, but ImportFromURL passes it straight through.
func enabledConfig() *training.ClaudeConfig {
	return &training.ClaudeConfig{Enabled: true, CLIPath: "claude", Model: "claude-sonnet-4-6"}
}

func TestImportFromURLSuccess(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{resp: claudeRecipeJSON}
	installStubClaude(t, stub)

	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, recipePageHTML)
	}))
	defer server.Close()

	parsed, err := ImportFromURL(context.Background(), enabledConfig(), server.URL+"/oppskrift")
	if err != nil {
		t.Fatalf("ImportFromURL: %v", err)
	}

	if parsed.Title != "Fiskegrateng" {
		t.Errorf("title = %q, want %q", parsed.Title, "Fiskegrateng")
	}
	if parsed.Notes != "Klassisk norsk hverdagsmiddag." {
		t.Errorf("notes = %q", parsed.Notes)
	}
	if parsed.Servings != 4 {
		t.Errorf("servings = %d, want 4", parsed.Servings)
	}
	if parsed.SourceURL != server.URL+"/oppskrift" {
		t.Errorf("source_url = %q, want %q", parsed.SourceURL, server.URL+"/oppskrift")
	}

	if len(parsed.Ingredients) != 2 {
		t.Fatalf("got %d ingredients, want 2", len(parsed.Ingredients))
	}
	first := parsed.Ingredients[0]
	if first.Quantity != 400 || first.Unit != "g" || first.Name != "torsk" || first.Text != "400 g torsk, i terninger" {
		t.Errorf("ingredient[0] = %+v", first)
	}
	second := parsed.Ingredients[1]
	if second.Quantity != 3 || second.Unit != "dl" || second.Name != "melk" || second.Text != "3 dl melk" {
		t.Errorf("ingredient[1] = %+v", second)
	}

	if len(parsed.Steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(parsed.Steps))
	}
	if parsed.Steps[0].Text != "Kok makaronien i 8 minutter." || parsed.Steps[0].DurationSeconds != 480 {
		t.Errorf("step[0] = %+v, want the timed step", parsed.Steps[0])
	}
	if parsed.Steps[1].Text != "Stek i ovnen." || parsed.Steps[1].DurationSeconds != 0 {
		t.Errorf("step[1] = %+v, want an untimed step", parsed.Steps[1])
	}

	if gotUserAgent != importUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUserAgent, importUserAgent)
	}
	if stub.calls != 1 {
		t.Fatalf("claude called %d times, want 1", stub.calls)
	}
	// The prompt must carry page text, not raw markup or script noise.
	if !strings.Contains(stub.prompt, "400 g torsk, i terninger") {
		t.Error("prompt is missing the ingredient text from the page")
	}
	if strings.Contains(stub.prompt, "should-not-appear") {
		t.Error("prompt leaked a non-JSON-LD <script> body")
	}
	if strings.Contains(stub.prompt, "color: red") {
		t.Error("prompt leaked a <style> body")
	}
	if strings.Contains(stub.prompt, "Reklame som ikke hoerer hjemme") {
		t.Error("prompt leaked <nav> content")
	}
	if strings.Contains(stub.prompt, "Kontakt oss") {
		t.Error("prompt leaked <footer> content")
	}
	if !strings.Contains(stub.prompt, server.URL+"/oppskrift") {
		t.Error("prompt is missing the source URL")
	}
}

func TestImportFromURLKeepsJSONLD(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{resp: claudeRecipeJSON}
	installStubClaude(t, stub)

	page := `<html><head><script type="application/ld+json">
	{"@type":"Recipe","name":"Fiskegrateng","recipeYield":"4"}
	</script></head><body><h1>Fiskegrateng</h1></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, page)
	}))
	defer server.Close()

	if _, err := ImportFromURL(context.Background(), enabledConfig(), server.URL); err != nil {
		t.Fatalf("ImportFromURL: %v", err)
	}
	if !strings.Contains(stub.prompt, `"@type":"Recipe"`) {
		t.Error("prompt dropped the schema.org JSON-LD block")
	}
}

func TestImportFromURLMalformedJSON(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{resp: "not json"}
	installStubClaude(t, stub)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, recipePageHTML)
	}))
	defer server.Close()

	parsed, err := ImportFromURL(context.Background(), enabledConfig(), server.URL)
	if !errors.Is(err, ErrParse) {
		t.Fatalf("err = %v, want ErrParse", err)
	}
	if !reflectEqualZero(parsed) {
		t.Errorf("expected a zero ParsedRecipe on failure, got %+v", parsed)
	}
}

func TestImportFromURLIncompleteRecipeIsParseError(t *testing.T) {
	allowLocalFetches(t)
	// Valid JSON, but describes no usable recipe — the "no recipe on this page"
	// answer the prompt asks for.
	stub := &stubClaude{resp: `{"title": "", "ingredients": [], "steps": []}`}
	installStubClaude(t, stub)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, recipePageHTML)
	}))
	defer server.Close()

	if _, err := ImportFromURL(context.Background(), enabledConfig(), server.URL); !errors.Is(err, ErrParse) {
		t.Fatalf("err = %v, want ErrParse", err)
	}
}

func TestImportFromURLStripsCodeFence(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{resp: "Here you go:\n```json\n" + claudeRecipeJSON + "\n```\n"}
	installStubClaude(t, stub)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, recipePageHTML)
	}))
	defer server.Close()

	parsed, err := ImportFromURL(context.Background(), enabledConfig(), server.URL)
	if err != nil {
		t.Fatalf("ImportFromURL: %v", err)
	}
	if parsed.Title != "Fiskegrateng" {
		t.Errorf("title = %q, want the fenced JSON to be parsed", parsed.Title)
	}
}

func TestImportFromURLFetchFailure(t *testing.T) {
	allowLocalFetches(t)

	t.Run("non-2xx status", func(t *testing.T) {
		stub := &stubClaude{resp: claudeRecipeJSON}
		installStubClaude(t, stub)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer server.Close()

		_, err := ImportFromURL(context.Background(), enabledConfig(), server.URL)
		if !errors.Is(err, ErrFetch) {
			t.Fatalf("err = %v, want ErrFetch", err)
		}
		if stub.calls != 0 {
			t.Errorf("claude called %d times, want 0 when the fetch fails", stub.calls)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		stub := &stubClaude{resp: claudeRecipeJSON}
		installStubClaude(t, stub)

		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := server.URL
		server.Close() // nothing is listening any more

		_, err := ImportFromURL(context.Background(), enabledConfig(), url)
		if !errors.Is(err, ErrFetch) {
			t.Fatalf("err = %v, want ErrFetch", err)
		}
		if stub.calls != 0 {
			t.Errorf("claude called %d times, want 0 when the fetch fails", stub.calls)
		}
	})
}

func TestImportFromURLClaudeFailure(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{err: errors.New("claude is not enabled")}
	installStubClaude(t, stub)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, recipePageHTML)
	}))
	defer server.Close()

	_, err := ImportFromURL(context.Background(), enabledConfig(), server.URL)
	if !errors.Is(err, ErrClaude) {
		t.Fatalf("err = %v, want ErrClaude", err)
	}
	if errors.Is(err, ErrParse) {
		t.Error("a CLI failure must not be reported as a parse failure")
	}
}

func TestImportFromURLTruncatesOversizedPage(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{resp: claudeRecipeJSON}
	installStubClaude(t, stub)

	// The recipe is served first, followed by filler that pushes the body well
	// past the byte cap. The request must still succeed on the leading content.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "<html><body><h1>Fiskegrateng</h1><p>400 g torsk</p>")
		filler := strings.Repeat("<p>filler filler filler</p>", 1024)
		for written := 0; written < maxPageBytes*2; written += len(filler) {
			fmt.Fprint(w, filler)
		}
		fmt.Fprint(w, "<p>MARKER-PAST-THE-CAP</p></body></html>")
	}))
	defer server.Close()

	parsed, err := ImportFromURL(context.Background(), enabledConfig(), server.URL)
	if err != nil {
		t.Fatalf("ImportFromURL on an oversized page: %v", err)
	}
	if parsed.Title != "Fiskegrateng" {
		t.Errorf("title = %q", parsed.Title)
	}
	if strings.Contains(stub.prompt, "MARKER-PAST-THE-CAP") {
		t.Error("content past the byte cap reached the prompt; the body was not truncated")
	}
	if !strings.Contains(stub.prompt, "400 g torsk") {
		t.Error("content before the byte cap was dropped")
	}
}

func TestImportFromURLRejectsBadURLs(t *testing.T) {
	stub := &stubClaude{resp: claudeRecipeJSON}
	installStubClaude(t, stub)

	for _, raw := range []string{"", "   ", "ftp://example.com/recipe", "file:///etc/passwd", "javascript:alert(1)", "not a url", "/relative/path"} {
		if _, err := ImportFromURL(context.Background(), enabledConfig(), raw); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("ImportFromURL(%q) err = %v, want ErrInvalidURL", raw, err)
		}
	}
	if stub.calls != 0 {
		t.Errorf("claude called %d times, want 0 for invalid URLs", stub.calls)
	}
}

func TestImportFromURLBlocksPrivateAddresses(t *testing.T) {
	// The dial guard is left in place here — this is the production behaviour.
	stub := &stubClaude{resp: claudeRecipeJSON}
	installStubClaude(t, stub)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, recipePageHTML)
	}))
	defer server.Close()

	_, err := ImportFromURL(context.Background(), enabledConfig(), server.URL)
	if !errors.Is(err, ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch for a loopback address", err)
	}
	if stub.calls != 0 {
		t.Errorf("claude called %d times, want 0", stub.calls)
	}
}

func TestHTMLToTextSkipsChrome(t *testing.T) {
	text := htmlToText(recipePageHTML)
	for _, want := range []string{"Fiskegrateng", "400 g torsk, i terninger", "Kok makaronien i 8 minutter."} {
		if !strings.Contains(text, want) {
			t.Errorf("extracted text is missing %q", want)
		}
	}
	for _, unwanted := range []string{"should-not-appear", "color: red", "Forsiden", "Kontakt oss"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("extracted text still contains %q", unwanted)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	// Multi-byte input: cutting must land on a rune boundary.
	got := truncateRunes("æøåæøå", 3)
	if got != "æøå" {
		t.Errorf("truncateRunes = %q, want %q", got, "æøå")
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Errorf("truncateRunes = %q, want %q", got, "abc")
	}
}

func TestParseRecipeJSONNormalizes(t *testing.T) {
	parsed, err := parseRecipeJSON(`{
		"title": "  Suppe  ",
		"notes": " god ",
		"servings": -2,
		"source_url": "https://evil.example/injected",
		"ingredients": [
			{"quantity": -1, "unit": " dl ", "name": " melk ", "text": ""},
			{"quantity": 0, "unit": "", "name": "", "text": "   "}
		],
		"steps": [
			{"text": " Kok. ", "duration_seconds": -5},
			{"text": "   ", "duration_seconds": 60}
		]
	}`)
	if err != nil {
		t.Fatalf("parseRecipeJSON: %v", err)
	}
	if parsed.Title != "Suppe" || parsed.Notes != "god" {
		t.Errorf("title/notes not trimmed: %+v", parsed)
	}
	if parsed.Servings != 0 {
		t.Errorf("servings = %d, want a negative value clamped to 0", parsed.Servings)
	}
	if parsed.SourceURL != "" {
		t.Errorf("source_url = %q, want the model's value discarded", parsed.SourceURL)
	}
	if len(parsed.Ingredients) != 1 {
		t.Fatalf("got %d ingredients, want the blank row dropped", len(parsed.Ingredients))
	}
	ing := parsed.Ingredients[0]
	if ing.Quantity != 0 || ing.Unit != "dl" || ing.Name != "melk" || ing.Text != "melk" {
		t.Errorf("ingredient = %+v, want trimmed values and text falling back to name", ing)
	}
	if len(parsed.Steps) != 1 {
		t.Fatalf("got %d steps, want the blank row dropped", len(parsed.Steps))
	}
	if parsed.Steps[0].Text != "Kok." || parsed.Steps[0].DurationSeconds != 0 {
		t.Errorf("step = %+v", parsed.Steps[0])
	}
}

// --- handler tests ---

// enableClaude turns on the Claude preference for a user so HandleImport gets
// past its config check.
func enableClaude(t *testing.T, env *testEnv, userID int64) {
	t.Helper()
	if err := auth.SetPreference(env.db, userID, "claude_enabled", "true"); err != nil {
		t.Fatalf("enable claude for user %d: %v", userID, err)
	}
}

// countRecipes reports how many recipe rows exist, so the tests can assert the
// import endpoint never persists anything.
func countRecipes(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow("SELECT COUNT(*) FROM recipes").Scan(&n); err != nil {
		t.Fatalf("count recipes: %v", err)
	}
	return n
}

func TestHandleImportSuccess(t *testing.T) {
	allowLocalFetches(t)
	installStubClaude(t, &stubClaude{resp: claudeRecipeJSON})

	env := setupHandlerTest(t)
	enableClaude(t, env, ownerID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, recipePageHTML)
	}))
	defer server.Close()

	rec := env.do(t, ownerID, http.MethodPost, "/api/recipes/import", `{"url":"`+server.URL+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Recipe ParsedRecipe `json:"recipe"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v (body = %s)", err, rec.Body.String())
	}
	if envelope.Recipe.Title != "Fiskegrateng" {
		t.Errorf("title = %q", envelope.Recipe.Title)
	}
	if len(envelope.Recipe.Ingredients) != 2 || len(envelope.Recipe.Steps) != 2 {
		t.Errorf("recipe = %+v", envelope.Recipe)
	}
	if envelope.Recipe.SourceURL != server.URL {
		t.Errorf("source_url = %q, want %q", envelope.Recipe.SourceURL, server.URL)
	}

	// The acceptance criterion: import is read-only.
	if n := countRecipes(t, env); n != 0 {
		t.Errorf("import persisted %d recipes, want 0", n)
	}
}

func TestHandleImportParseFailureIs422AndPersistsNothing(t *testing.T) {
	allowLocalFetches(t)
	installStubClaude(t, &stubClaude{resp: "not json"})

	env := setupHandlerTest(t)
	enableClaude(t, env, ownerID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, recipePageHTML)
	}))
	defer server.Close()

	rec := env.do(t, ownerID, http.MethodPost, "/api/recipes/import", `{"url":"`+server.URL+`"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body = %s", rec.Code, rec.Body.String())
	}
	if n := countRecipes(t, env); n != 0 {
		t.Errorf("a failed import left %d recipes behind, want 0", n)
	}
	var ingredients int
	if err := env.db.QueryRow("SELECT COUNT(*) FROM recipe_ingredients").Scan(&ingredients); err != nil {
		t.Fatalf("count ingredients: %v", err)
	}
	if ingredients != 0 {
		t.Errorf("a failed import left %d ingredients behind, want 0", ingredients)
	}
}

func TestHandleImportFetchFailureIs502(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{resp: claudeRecipeJSON}
	installStubClaude(t, stub)

	env := setupHandlerTest(t)
	enableClaude(t, env, ownerID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer server.Close()

	rec := env.do(t, ownerID, http.MethodPost, "/api/recipes/import", `{"url":"`+server.URL+`"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body = %s", rec.Code, rec.Body.String())
	}
	if stub.calls != 0 {
		t.Errorf("claude called %d times, want 0", stub.calls)
	}
	if n := countRecipes(t, env); n != 0 {
		t.Errorf("a failed import left %d recipes behind, want 0", n)
	}
}

func TestHandleImportRejectsBadRequests(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{resp: claudeRecipeJSON}
	installStubClaude(t, stub)

	env := setupHandlerTest(t)
	enableClaude(t, env, ownerID)

	cases := []struct {
		name string
		body string
	}{
		{"not json", `{`},
		{"missing url", `{}`},
		{"non-http scheme", `{"url":"file:///etc/passwd"}`},
		{"relative url", `{"url":"/oppskrift"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, ownerID, http.MethodPost, "/api/recipes/import", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
	if stub.calls != 0 {
		t.Errorf("claude called %d times, want 0", stub.calls)
	}
}

func TestHandleImportRequiresClaudeEnabled(t *testing.T) {
	allowLocalFetches(t)
	stub := &stubClaude{resp: claudeRecipeJSON}
	installStubClaude(t, stub)

	env := setupHandlerTest(t) // claude_enabled is not set for this user

	rec := env.do(t, ownerID, http.MethodPost, "/api/recipes/import", `{"url":"https://example.com/recipe"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	if stub.calls != 0 {
		t.Errorf("claude called %d times, want 0", stub.calls)
	}
}

// reflectEqualZero reports whether p carries no data at all.
func reflectEqualZero(p ParsedRecipe) bool {
	return p.Title == "" && p.Notes == "" && p.Servings == 0 && p.SourceURL == "" &&
		len(p.Ingredients) == 0 && len(p.Steps) == 0
}
