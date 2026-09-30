package integration_test

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/render"
)

// Keep the existing public content region identical in preview. Publishing
// tickets can reuse this assertion when public snapshots start changing.
var admin8MainRE = regexp.MustCompile(`(?s)<main\b[^>]*>.*?</main>`)
var admin8TextRE = regexp.MustCompile(`<[^>]*>`)
var admin8AnchorRE = regexp.MustCompile(`(?is)<a\b[^>]*>.*?</a>`)

func admin8Content(t *testing.T, page string) string {
	t.Helper()
	regions := admin8MainRE.FindAllString(page, -1)
	if len(regions) != 1 {
		t.Errorf("expected one shared public main region, got %d", len(regions))
		return ""
	}
	return regions[0]
}

func admin8Text(page string) string {
	return strings.Join(strings.Fields(html.UnescapeString(admin8TextRE.ReplaceAllString(page, " "))), " ")
}

func admin8Headers(t *testing.T, headers http.Header) {
	t.Helper()
	for key, want := range map[string]string{
		"Cache-Control": "no-store", "X-Robots-Tag": "noindex", "Content-Type": "text/html; charset=utf-8",
	} {
		if got := headers.Values(key); len(got) != 1 || got[0] != want {
			t.Errorf("%s=%q, want exactly %q", key, got, want)
		}
	}
}

func admin8Parity(t *testing.T, page, title, body string) {
	t.Helper()
	expected, err := render.Page(content.Snapshot{Title: title, Fields: map[string]string{"body": body}})
	if err != nil {
		t.Fatal(err)
	}
	region := admin8Content(t, page)
	if want := admin8Content(t, string(expected)); region != want {
		t.Errorf("preview content=%q, want public renderer content=%q", region, want)
	}
	if strings.Contains(page, "<script>") || strings.Contains(page, "<em>") {
		t.Error("stored plain text became HTML markup")
	}
	outside := strings.Replace(page, region, "", 1)
	if !strings.Contains(strings.ToLower(admin8Text(outside)), "preview") {
		t.Error("missing clear preview context outside shared public content")
	}
	if !strings.Contains(page, "white-space: pre-line") {
		t.Error("preview lost the public plain-text newline presentation")
	}
}

func TestPhase1PreviewDraft(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	firstTitle := `First <script>title & "quote"</script>`
	firstBody := "\nFirst <script>body</script>\nSecond & line"
	publishedFixture(t, db, "/preview-parity", firstTitle, firstBody)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Initial draft", "/preview-private")
	previewPath := "/admin/pages/" + id + "/preview"
	_, public := admin6HTTP(t, base, "GET", "/preview-parity", "", nil, 200)

	for i, v := range []url.Values{
		{"title": {firstTitle}, "path": {"/preview-private"}, "body": {strings.ReplaceAll(firstBody, "\n", "\r\n")}},
		{"title": {`Second <em>title</em> & text`}, "path": {"/preview-private"}, "body": {"Second <em>body</em>\r\nNext & line\rLast"}},
	} {
		v.Set("draft_revision", fmt.Sprint(i+1))
		admin7Post(t, base, id, v, 303)
		if got := scalar[int](t, db, "SELECT draft_revision FROM items WHERE id='"+id+"'"); got != i+2 {
			t.Fatalf("save %d revision=%d, want %d", i+1, got, i+2)
		}
		before := admin6State(t, db)
		t.Run(fmt.Sprintf("saved_version_%d", i+1), func(t *testing.T) {
			h, page := admin6HTTP(t, base, "GET", previewPath, "", nil, 200)
			admin8Headers(t, h)
			admin8Parity(t, page, v.Get("title"), admin6Newlines(v.Get("body")))
			if i == 0 && admin8Content(t, page) != admin8Content(t, public) {
				t.Error("preview differs from equivalent real published fixture")
			}
			admin6Unchanged(t, db, before)
			query := url.Values{"title": {"UNSAVED TITLE"}, "body": {"UNSAVED BODY"}, "path": {"/example"}, "id": {"not-the-url-item"}, "draft_revision": {"1"}}
			_, spoofed := admin6HTTP(t, base, "GET", previewPath+"?"+query.Encode(), "", nil, 200)
			if spoofed != page || strings.Contains(spoofed, "UNSAVED") {
				t.Error("unsaved query input selected preview content")
			}
			admin6Unchanged(t, db, before)
			h, body := admin6HTTP(t, base, "HEAD", previewPath, "", nil, 200)
			admin8Headers(t, h)
			if body != "" {
				t.Errorf("HEAD body=%q, want empty", body)
			}
			admin6Unchanged(t, db, before)
			admin6HTTP(t, base, "GET", "/preview-private", "", nil, 404)
			admin6Unchanged(t, db, before)
		})
	}

	t.Run("published_page_uses_saved_draft", func(t *testing.T) {
		seedID := scalar[string](t, db, "SELECT id FROM items WHERE path='/example'")
		_, oldPublic := admin6HTTP(t, base, "GET", "/example", "", nil, 200)
		v := url.Values{"title": {"Saved private <em>seed</em>"}, "path": {"/example"}, "body": {"Private & body\nnext"}, "draft_revision": {"1"}}
		admin7Post(t, base, seedID, v, 303)
		before := admin6State(t, db)
		_, page := admin6HTTP(t, base, "GET", "/admin/pages/"+seedID+"/preview", "", nil, 200)
		admin8Parity(t, page, v.Get("title"), v.Get("body"))
		_, stillPublic := admin6HTTP(t, base, "GET", "/example", "", nil, 200)
		if stillPublic != oldPublic {
			t.Error("preview changed the previously published seed")
		}
		admin6Unchanged(t, db, before)
	})
}

func TestPhase1PreviewDraftHTTPBoundary(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Draft", "/preview-private")
	path := "/admin/pages/" + id + "/preview"
	before := admin6State(t, db)
	for _, method := range []string{"GET", "HEAD"} {
		t.Run("missing_"+method, func(t *testing.T) {
			_, body := admin6HTTP(t, base, method, "/admin/pages/019f0000-0000-7000-8000-000000000099/preview", "", nil, 404)
			if method == "HEAD" && body != "" {
				t.Error("missing-item HEAD returned a body")
			}
			admin6Unchanged(t, db, before)
		})
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		t.Run("wrong_method_"+method, func(t *testing.T) {
			h, _ := admin6HTTP(t, base, method, path, "title=Attempted+write&body=attack", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 405)
			if got := h.Values("Allow"); len(got) != 1 || got[0] != "GET, HEAD" {
				t.Errorf("Allow=%q, want exactly GET, HEAD", got)
			}
			admin6Unchanged(t, db, before)
		})
	}
	for _, tc := range []struct {
		name, method string
		headers      map[string]string
		status       int
	}{
		{"bad_host_get", "GET", map[string]string{"Host": "attacker.example"}, 400},
		{"bad_host_head", "HEAD", map[string]string{"Host": "attacker.example"}, 400},
		{"origin_post", "POST", map[string]string{"Origin": "https://attacker.example"}, 403},
		{"fetch_metadata_post", "POST", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admin6HTTP(t, base, tc.method, path, "", tc.headers, tc.status)
			admin6Unchanged(t, db, before)
		})
	}
}

func TestPhase1PreviewSavedDraftLink(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Draft", "/preview-private")
	before := admin6State(t, db)
	_, page := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/edit", "", nil, 200)
	target := "/admin/pages/" + id + "/preview"
	found := false
	for _, anchor := range admin8AnchorRE.FindAllString(page, -1) {
		tag := anchor[:strings.Index(anchor, ">")+1]
		a := admin6Attrs(tag)
		if a["href"] != target {
			continue
		}
		found = true
		if admin8Text(anchor) != "Preview saved draft" {
			t.Errorf("preview link label=%q, want Preview saved draft", admin8Text(anchor))
		}
		if _, ok := a["onclick"]; ok {
			t.Error("preview must be an ordinary GET link")
		}
	}
	if !found {
		t.Error("missing ordinary Preview saved draft link to saved URL item")
	}
	text := strings.ToLower(admin8Text(page))
	if !strings.Contains(text, "latest saved draft") {
		t.Error("edit form does not explain preview uses the latest saved draft")
	}
	if !regexp.MustCompile(`save\b[^.]*\bbefore\b[^.]*\bpreview`).MatchString(text) {
		t.Error("edit form lacks explicit save-before-preview instructions")
	}
	_, create := admin6HTTP(t, base, "GET", "/admin/pages/new", "", nil, 200)
	if strings.Contains(strings.ToLower(create), "/preview") || strings.Contains(strings.ToLower(admin8Text(create)), "preview saved draft") {
		t.Error("create form offers preview before an item exists")
	}
	admin6Unchanged(t, db, before)
}
