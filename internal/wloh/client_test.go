package wloh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeSite mimics the parts of the league site the client uses, with the
// same element ids and field names as the real markup.
type fakeSite struct {
	players  []string
	results  map[string][2]int // "home|away" -> scores
	email    string            // accepted for any listed player
	rejectAs string            // notice shown instead of registering a result
	posts    int
}

const fakeSerie = 965530

func (f *fakeSite) page(loggedInAs string) string {
	var sb strings.Builder
	sb.WriteString(`<html><body><form method="post" action="./serie-965530" id="Form2">
<input type="hidden" name="__VIEWSTATE" id="__VIEWSTATE" value="state" />
<input type="hidden" name="__EVENTVALIDATION" id="__EVENTVALIDATION" value="valid" />`)
	if loggedInAs != "" {
		fmt.Fprintf(&sb, `<span id="HeadLoginView_HeadLoginName">%s</span>`, loggedInAs)
	}
	sb.WriteString(`<select name="ctl00$MainContent$DropDownList4Spraak" id="MainContent_DropDownList4Spraak">
<option selected="selected" value="1">Wordfeud Norsk</option><option value="2">Wordfeud English International</option></select>
<select name="ctl00$MainContent$DropDownList5Sesong" id="MainContent_DropDownList5Sesong">
<option selected="selected" value="398">398</option><option value="397">397</option></select>
<select name="ctl00$MainContent$DropDownList6Divisjon" id="MainContent_DropDownList6Divisjon">
<option value="4">4</option><option selected="selected" value="5">5</option></select>
<select name="ctl00$MainContent$DropDownList3Avdeling" id="MainContent_DropDownList3Avdeling">
<option value="965531">10</option><option selected="selected" value="965530">11</option></select>
<input name="ctl00$MainContent$TextBoxFinnspiller" type="text" maxlength="60" id="MainContent_TextBoxFinnspiller" />
<div id="MainContent_DivTabell"><table><thead><tr><th>Plass</th><th>Navn</th></tr></thead>`)
	for i, name := range f.players {
		played, forPts, agPts := 0, 0, 0
		for _, opp := range f.players {
			if r, ok := f.results[name+"|"+opp]; ok {
				played, forPts, agPts = played+1, forPts+r[0], agPts+r[1]
			}
		}
		fmt.Fprintf(&sb, `<tr><td><span>%d</span></td><td><a href="https://wordfeud.aasmul.net/bruker-%d">%s</a></td><td><span> %d </span></td><td><span>0</span></td><td><span>0</span></td><td><span>0</span></td><td><span>%d - %d</span></td><td><span>%d</span></td><td><span>0</span></td><td><span>Bergen</span></td></tr>`,
			i+1, 100+i, name, played, forPts, agPts, forPts-agPts)
	}
	sb.WriteString(`</table></div>
<span id="MainContent_Label58Deadline" class="tekstlabel">Deadline: 23/10 2026 23:59</span>`)
	if loggedInAs != "" {
		for _, sel := range []string{"DropDownList2Hjemmespiller", "DropDownList1Bortespiller"} {
			fmt.Fprintf(&sb, `<select name="ctl00$MainContent$%s" id="MainContent_%s">`, sel, sel)
			for i, name := range f.players {
				fmt.Fprintf(&sb, `<option value="%d">%s</option>`, i+1, name)
			}
			sb.WriteString(`</select>`)
		}
		sb.WriteString(`<input name="ctl00$MainContent$TextBox2" type="text" id="MainContent_TextBox2" />
<input name="ctl00$MainContent$TextBox4" type="text" id="MainContent_TextBox4" />
<input type="submit" name="ctl00$MainContent$butSend" value="Send result" id="MainContent_butSend" />`)
	}
	fmt.Fprintf(&sb, `<span id="MainContent_Label55" style="color:Red;">%s</span>`, f.rejectAs)
	sb.WriteString(`<div id="MainContent_DivKampliste"><table><tbody>`)
	row := 0
	for _, home := range f.players {
		fmt.Fprintf(&sb, `<tr><td><span id = "MainContent_ListView1_spelar_%d"><b>%s</b></span></td><td><span id = "MainContent_ListView1_resultat_%d"></span></td></tr>`, row, strings.ToUpper(home), row)
		row++
		for _, away := range f.players {
			if away == home {
				continue
			}
			res := ""
			if r, ok := f.results[home+"|"+away]; ok {
				res = fmt.Sprintf(`<font color = "#009933">%d – %d</font>`, r[0], r[1])
			}
			fmt.Fprintf(&sb, `<tr><td><span id = "MainContent_ListView1_spelar_%d">%s – %s</span></td><td><span id = "MainContent_ListView1_resultat_%d">%s</span></td></tr>`, row, home, away, row, res)
			row++
		}
	}
	sb.WriteString(`</tbody></table></div></form></body></html>`)
	return sb.String()
}

func (f *fakeSite) loggedIn(r *http.Request) string {
	if c, err := r.Cookie(".ASPXAUTH"); err == nil {
		return c.Value
	}
	return ""
}

func (f *fakeSite) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/Account/Login.aspx", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			user, email := r.FormValue("ctl00$MainContent$TextBox1"), r.FormValue("ctl00$MainContent$TextBox2")
			if r.FormValue("__VIEWSTATE") != "login-state" || r.FormValue("ctl00$MainContent$Button1") == "" {
				http.Error(w, "bad postback", http.StatusInternalServerError)
				return
			}
			for _, p := range f.players {
				if strings.EqualFold(p, user) && email == f.email {
					http.SetCookie(w, &http.Cookie{Name: ".ASPXAUTH", Value: user, Path: "/"})
					fmt.Fprint(w, f.page(user))
					return
				}
			}
		}
		fmt.Fprint(w, `<html><body><form method="post" action="./Login.aspx">
<input type="hidden" name="__VIEWSTATE" value="login-state" />
<input name="ctl00$MainContent$TextBox1" type="text" /><input name="ctl00$MainContent$TextBox2" type="email" />
<input type="submit" name="ctl00$MainContent$Button1" value="Log on" /></form></body></html>`)
	})
	mux.HandleFunc("/Default.aspx", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if r.FormValue("__VIEWSTATE") != "state" || r.FormValue("ctl00$MainContent$ButtonFinnspiller") == "" {
				http.Error(w, "bad postback", http.StatusInternalServerError)
				return
			}
			if r.FormValue("ctl00$MainContent$TextBoxFinnspiller") == "nobody" {
				other := &fakeSite{players: []string{"someone", "else"}}
				fmt.Fprint(w, other.page(""))
				return
			}
		}
		fmt.Fprint(w, f.page(f.loggedIn(r)))
	})
	mux.HandleFunc("/serie-"+strconv.Itoa(fakeSerie), func(w http.ResponseWriter, r *http.Request) {
		user := f.loggedIn(r)
		if r.Method == http.MethodPost {
			f.posts++
			if user == "" || r.FormValue("__EVENTVALIDATION") != "valid" || r.FormValue("ctl00$MainContent$butSend") == "" {
				http.Error(w, "bad postback", http.StatusInternalServerError)
				return
			}
			if f.rejectAs == "" {
				hi, _ := strconv.Atoi(r.FormValue("ctl00$MainContent$DropDownList2Hjemmespiller"))
				ai, _ := strconv.Atoi(r.FormValue("ctl00$MainContent$DropDownList1Bortespiller"))
				hs, _ := strconv.Atoi(r.FormValue("ctl00$MainContent$TextBox2"))
				as, _ := strconv.Atoi(r.FormValue("ctl00$MainContent$TextBox4"))
				home, away := f.players[hi-1], f.players[ai-1]
				f.results[home+"|"+away] = [2]int{hs, as}
				f.results[away+"|"+home] = [2]int{as, hs}
			}
		}
		fmt.Fprint(w, f.page(user))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	return mux
}

func newFake(t *testing.T) (*fakeSite, *Client) {
	t.Helper()
	f := &fakeSite{
		players: []string{"tjobias", "ave-62", "Luremus"},
		results: map[string][2]int{"tjobias|ave-62": {469, 480}, "ave-62|tjobias": {480, 469}},
		email:   "me@example.com",
	}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return f, &Client{HTTP: srv.Client(), BaseURL: srv.URL}
}

func TestFindGroup(t *testing.T) {
	_, c := newFake(t)
	g, err := c.FindGroup(context.Background(), "luremus")
	if err != nil {
		t.Fatalf("FindGroup: %v", err)
	}
	if g.SerieID != fakeSerie || g.Season != 398 || g.Division != 5 || g.GroupNo != 11 || g.LeagueID != 1 || g.League != "Wordfeud Norsk" {
		t.Errorf("group header = %+v", g)
	}
	want := time.Date(2026, 10, 23, 23, 59, 0, 0, zone())
	if !g.Deadline.Equal(want) {
		t.Errorf("deadline = %v, want %v", g.Deadline, want)
	}
	if len(g.Standings) != 3 || g.Standings[2].Name != "Luremus" || g.Standings[2].From != "Bergen" {
		t.Errorf("standings = %+v", g.Standings)
	}
	if s := g.Standings[1]; s.Played != 1 || s.ScoreFor != 480 || s.ScoreAg != 469 || s.Margin != 11 {
		t.Errorf("ave-62 row = %+v", s)
	}
	if len(g.Matches) != 2 {
		t.Fatalf("matches = %+v", g.Matches)
	}
	// A dash inside a username must not split the pairing.
	if my, opp, ok := g.Result("AVE-62", "tjobias"); !ok || my != 480 || opp != 469 {
		t.Errorf("Result(ave-62, tjobias) = %d %d %v", my, opp, ok)
	}
	if _, _, ok := g.Result("Luremus", "tjobias"); ok {
		t.Error("unplayed match reported as played")
	}
	if !strings.HasSuffix(g.URL, "/serie-965530") {
		t.Errorf("url = %q", g.URL)
	}
}

func TestFindGroupUnknownPlayer(t *testing.T) {
	_, c := newFake(t)
	if _, err := c.FindGroup(context.Background(), "nobody"); !errors.Is(err, ErrPlayerNotFound) {
		t.Errorf("err = %v, want ErrPlayerNotFound", err)
	}
}

func TestGetGroup(t *testing.T) {
	_, c := newFake(t)
	g, err := c.GetGroup(context.Background(), fakeSerie)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if g.Player("LUREMUS") != "Luremus" {
		t.Errorf("Player lookup failed: %+v", g.Standings)
	}
}

func TestCheckLogin(t *testing.T) {
	_, c := newFake(t)
	if err := c.CheckLogin(context.Background(), "luremus", "me@example.com"); err != nil {
		t.Errorf("valid login: %v", err)
	}
	if err := c.CheckLogin(context.Background(), "luremus", "wrong@example.com"); !errors.Is(err, ErrLoginFailed) {
		t.Errorf("err = %v, want ErrLoginFailed", err)
	}
}

func TestSubmitResult(t *testing.T) {
	f, c := newFake(t)
	g, err := c.SubmitResult(context.Background(), "luremus", "me@example.com", fakeSerie, "TJOBIAS", 592, 440)
	if err != nil {
		t.Fatalf("SubmitResult: %v", err)
	}
	if got := f.results["Luremus|tjobias"]; got != [2]int{592, 440} {
		t.Errorf("site recorded %v", got)
	}
	if my, opp, ok := g.Result("luremus", "tjobias"); !ok || my != 592 || opp != 440 {
		t.Errorf("returned group result = %d %d %v", my, opp, ok)
	}
}

func TestSubmitResultRejected(t *testing.T) {
	f, c := newFake(t)
	f.rejectAs = "Result already registered"
	_, err := c.SubmitResult(context.Background(), "luremus", "me@example.com", fakeSerie, "tjobias", 592, 440)
	var re *ResultError
	if !errors.As(err, &re) || re.Notice != "Result already registered" || !errors.Is(err, ErrNotRegistered) {
		t.Errorf("err = %v", err)
	}
}

func TestSubmitResultBadLoginPostsNothing(t *testing.T) {
	f, c := newFake(t)
	_, err := c.SubmitResult(context.Background(), "luremus", "wrong@example.com", fakeSerie, "tjobias", 592, 440)
	if !errors.Is(err, ErrLoginFailed) {
		t.Errorf("err = %v, want ErrLoginFailed", err)
	}
	if f.posts != 0 {
		t.Errorf("posted %d results after a failed login", f.posts)
	}
}

func TestSubmitResultUnknownOpponent(t *testing.T) {
	f, c := newFake(t)
	if _, err := c.SubmitResult(context.Background(), "luremus", "me@example.com", fakeSerie, "stranger", 1, 2); err == nil {
		t.Error("expected an error for an opponent outside the group")
	}
	if f.posts != 0 {
		t.Errorf("posted %d results for an unknown opponent", f.posts)
	}
}
