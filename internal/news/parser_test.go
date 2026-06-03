package news

import "testing"

const vgSample = `<?xml version="1.0"?><rss xmlns:vg="http://www.vg.no/namespace" version="2.0"><channel>
<item>
  <title>Streik truer rett før VM</title>
  <pubDate>Wed, 03 Jun 2026 10:08:10 GMT</pubDate>
  <description>Rundt 2000 arbeidere &lt;b&gt;ved&lt;/b&gt; stadion vurderer streik.</description>
  <link>https://www.vg.no/sport/i/0pLmeg/streik</link>
  <guid>https://www.vg.no/i/0pLmeg</guid>
  <category>Sport</category>
  <vg:img>https://img.example/a.jpg</vg:img>
  <enclosure url="https://img.example/a.jpg" type="img/jpg"/>
</item>
</channel></rss>`

const tv2Sample = `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel>
<item>
  <title>NRK-kommentator slutter</title>
  <link>https://www.tv2.no/a/18905613</link>
  <description>Cecilie slutter som kommentator.</description>
  <enclosure url="https://cdn.tv2.no/x.jpg" length="1" type="image/jpg" />
  <category domain="section">Nyheter</category>
  <category domain="tags">nyheter, medier, innenriks</category>
  <pubDate>Wed, 03 Jun 2026 08:09:11 GMT</pubDate>
  <guid>https://www.tv2.no/a/18905613</guid>
</item>
</channel></rss>`

const gamerSample = `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/"><channel>
<item>
  <title><![CDATA[PlayStation-show oppsummert]]></title>
  <link>https://www.gamer.no/artikler/x/520500</link>
  <description><![CDATA[]]></description>
  <guid>https://www.gamer.no/artikler/x/520500</guid>
  <pubDate>Wed, 03 Jun 2026 09:34:00 +0200</pubDate>
  <media:content url="https://i.bo3.no/x.jpg" type="image/jpeg" />
</item>
</channel></rss>`

func TestParseFeedVG(t *testing.T) {
	arts, err := parseFeed(Source{Key: "vg", Name: "VG"}, []byte(vgSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Fatalf("want 1 article, got %d", len(arts))
	}
	a := arts[0]
	if a.Title != "Streik truer rett før VM" {
		t.Errorf("title = %q", a.Title)
	}
	if a.Summary != "Rundt 2000 arbeidere ved stadion vurderer streik." {
		t.Errorf("summary not cleaned: %q", a.Summary)
	}
	if a.ImageURL != "https://img.example/a.jpg" {
		t.Errorf("image = %q", a.ImageURL)
	}
	if a.PublishedAt.IsZero() {
		t.Error("date not parsed")
	}
	if len(a.Categories) != 1 || a.Categories[0] != "Sport" {
		t.Errorf("categories = %v", a.Categories)
	}
	if a.ID == "" {
		t.Error("empty id")
	}
}

func TestParseFeedTV2SplitsCommaCategories(t *testing.T) {
	arts, _ := parseFeed(Source{Key: "tv2", Name: "TV 2"}, []byte(tv2Sample))
	if len(arts) != 1 {
		t.Fatalf("want 1, got %d", len(arts))
	}
	cats := arts[0].Categories
	// "Nyheter" (section) + "nyheter, medier, innenriks" (tags) → deduped
	// case-insensitively to: Nyheter, medier, innenriks = 3.
	if len(cats) != 3 {
		t.Errorf("expected 3 deduped categories, got %v", cats)
	}
	if arts[0].ImageURL == "" {
		t.Error("tv2 enclosure image missing")
	}
}

func TestParseFeedGamerCDATAAndOffsetDate(t *testing.T) {
	arts, _ := parseFeed(Source{Key: "gamer", Name: "gamer.no"}, []byte(gamerSample))
	if len(arts) != 1 {
		t.Fatalf("want 1, got %d", len(arts))
	}
	if arts[0].Title != "PlayStation-show oppsummert" {
		t.Errorf("CDATA title = %q", arts[0].Title)
	}
	if arts[0].PublishedAt.IsZero() {
		t.Error("+0200 offset date not parsed")
	}
	if arts[0].ImageURL != "https://i.bo3.no/x.jpg" {
		t.Errorf("media:content image = %q", arts[0].ImageURL)
	}
}

func TestArticleIDStable(t *testing.T) {
	if ArticleID("https://x/y") != ArticleID("https://x/y") {
		t.Error("ArticleID not stable")
	}
	if ArticleID("a") == ArticleID("b") {
		t.Error("ArticleID collision")
	}
}
