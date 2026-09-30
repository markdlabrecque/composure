package integration_test

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestTicket26PublishedEditRetainsRepublishControl(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Published A", "/republish-control")
	admin9Publish(t, base, id, 1, 303)
	admin7Post(t, base, id, url.Values{
		"title": {"Saved B"}, "path": {"/republish-control"},
		"body": {"Private B body"}, "draft_revision": {"1"},
	}, 303)
	if scalar[int](t, db, "SELECT published_snapshot_id IS NOT NULL FROM items WHERE id='"+id+"'") != 1 {
		t.Fatal("fixture Page must remain published after saving draft B")
	}
	revision := scalar[int](t, db, "SELECT draft_revision FROM items WHERE id='"+id+"'")
	_, page := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/edit", "", nil, 200)
	forms := regexp.MustCompile(`(?is)<form\b[^>]*>.*?</form>`)
	for _, form := range forms.FindAllString(page, -1) {
		tags := admin6TagRE.FindAllString(form, -1)
		attrs := admin6Attrs(tags[0])
		if attrs["action"] != "/admin/pages/"+id+"/publish" || !strings.EqualFold(attrs["method"], "post") {
			continue
		}
		currentRevision, submitButton := false, false
		for _, tag := range tags {
			a := admin6Attrs(tag)
			if strings.HasPrefix(strings.ToLower(tag), "<input") && a["type"] == "hidden" && a["name"] == "draft_revision" && a["value"] == fmt.Sprint(revision) {
				currentRevision = true
			}
			if strings.HasPrefix(strings.ToLower(tag), "<button") && a["type"] == "submit" {
				_, disabled := a["disabled"]
				submitButton = !disabled
			}
		}
		if !currentRevision {
			t.Errorf("republish POST form lacks hidden current draft_revision=%d", revision)
		}
		if !submitButton || !strings.Contains(form, ">Publish saved draft</button>") {
			t.Error("republish POST form lacks enabled Publish saved draft submit control")
		}
		return
	}
	t.Fatal("published edit form lacks POST publish action for saved draft B")
}
