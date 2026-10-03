// Package web owns loopback listening and the public HTTP boundary.
package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/markdlabrecque/composure/internal/config"
	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/render"
)

const adminBodyLimit = 1 << 20

//go:embed admin.css
var adminCSS string

//go:embed admin_list.html admin_form.html admin_saved.html
var adminTemplates embed.FS

var pageListTemplate = template.Must(template.ParseFS(adminTemplates, "admin_list.html"))
var pageFormTemplate = template.Must(template.ParseFS(adminTemplates, "admin_form.html"))

type formField struct {
	ID, Kind, Label, HelpText, Value, TextareaValue string
	Required                                        bool
}

type formProblem struct {
	Field, Code, Message string
}

type conflictOwner struct {
	ID, Title string
}

type pageFormData struct {
	Heading, Action, Revision, SubmitLabel string
	CSRFToken                              string
	Notice                                 string
	PreviewURL                             string
	PublishURL                             string
	Fields                                 []formField
	Values                                 map[string]string
	Errors                                 []formProblem
	Owner                                  *conflictOwner
}

// ResolveLoopback validates every DNS result and returns an exact bind address.
func ResolveLoopback(ctx context.Context, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid address %q", address)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return "", fmt.Errorf("invalid port %q", port)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("cannot resolve address %q", address)
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() || ip.Zone != "" {
			return "", fmt.Errorf("address %s is not loopback; phase 1 serves only 127.0.0.1, ::1 or localhost", address)
		}
	}
	return net.JoinHostPort(ips[0].IP.String(), port), nil
}

func Handler(repository content.Repository, port string) http.Handler {
	return HandlerWithClock(repository, port, time.Now)
}

// HandlerWithClock constructs the server handler with an explicit server clock.
// The clock is shared with routes whose persisted values depend on server time.
func HandlerWithClock(repository content.Repository, port string, now func() time.Time) http.Handler {
	mux := http.NewServeMux()
	handleAdminPost := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, authenticatedPost(handler))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})
	registerSignInRoutes(mux, repository)
	registerInvitationRoutes(mux, repository, handleAdminPost, now)
	handleAdminPost("POST /admin/sign-out", func(w http.ResponseWriter, r *http.Request) { serveSignOut(w, r, repository) })
	mux.HandleFunc("GET /admin/static/admin.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write([]byte(adminCSS))
	})
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /admin/pages", func(w http.ResponseWriter, r *http.Request) {
		items, err := repository.ListItems(r.Context(), "page")
		if err != nil {
			http.Error(w, "cannot load Pages", http.StatusInternalServerError)
			return
		}
		writeTemplate(w, pageListTemplate, items)
	})
	mux.HandleFunc("GET /admin/pages/new", func(w http.ResponseWriter, r *http.Request) {
		definition, err := activePageDefinition(r.Context(), repository)
		if err != nil {
			http.Error(w, "cannot load Page configuration", http.StatusInternalServerError)
			return
		}
		writePageForm(w, r, http.StatusOK, newFormData(definition, nil, nil, nil))
	})
	handleAdminPost("POST /admin/pages", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := pageWriteActor(w, r)
		if !ok {
			return
		}
		definition, err := activePageDefinition(r.Context(), repository)
		if err != nil {
			http.Error(w, "cannot load Page configuration", http.StatusInternalServerError)
			return
		}
		values := submittedValues(r, definition)
		draft, problems := content.PreparePageDraft(definition, values)
		if len(problems) > 0 {
			writeValidationForm(w, r, definition, values, problems)
			return
		}
		id, err := repository.CreateItemByActor(r.Context(), draft, time.Now(), actor)
		if err != nil {
			var taken *content.PathTakenError
			if errors.As(err, &taken) {
				owner := &conflictOwner{ID: taken.OwnerID, Title: taken.OwnerTitle}
				data := newFormData(definition, values, nil, owner)
				writePageForm(w, r, http.StatusConflict, data)
				return
			}
			http.Error(w, "cannot create Page", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/pages/"+id+"/edit", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /admin/pages/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		item, err := repository.GetItem(r.Context(), r.PathValue("id"))
		if errors.Is(err, content.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "cannot load Page", http.StatusInternalServerError)
			return
		}
		definition, err := activePageDefinition(r.Context(), repository)
		if err != nil {
			http.Error(w, "cannot load Page configuration", http.StatusInternalServerError)
			return
		}
		data := editFormData(item, definition, nil, nil, strconv.Itoa(item.Revision))
		if r.URL.Query().Get("notice") == "published" {
			data.Notice = "Page published."
		}
		writePageForm(w, r, http.StatusOK, data)
	})
	mux.HandleFunc("GET /admin/pages/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		item, err := repository.GetItem(r.Context(), r.PathValue("id"))
		if errors.Is(err, content.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "cannot load Page", http.StatusInternalServerError)
			return
		}
		definition, err := activePageDefinition(r.Context(), repository)
		if err != nil {
			http.Error(w, "cannot load Page configuration", http.StatusInternalServerError)
			return
		}
		html, err := render.PreviewWithDefinition(content.Snapshot{ItemID: item.ID, Title: item.Title, Path: item.Path, Fields: item.Fields}, definition)
		if err != nil {
			http.Error(w, "cannot render Page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Robots-Tag", "noindex")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	handleAdminPost("POST /admin/pages/{id}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := pageWriteActor(w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")
		item, err := repository.GetItem(r.Context(), id)
		if errors.Is(err, content.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "cannot load Page", http.StatusInternalServerError)
			return
		}
		definition, err := activePageDefinition(r.Context(), repository)
		if err != nil {
			http.Error(w, "cannot load Page configuration", http.StatusInternalServerError)
			return
		}
		values := submittedValues(r, definition)
		revisionText := r.PostForm.Get("draft_revision")
		revision, parseErr := strconv.Atoi(revisionText)
		if parseErr != nil {
			writeEditProblem(w, r, http.StatusConflict, item, definition, values, revisionText,
				formProblem{Code: "stale_draft", Message: "This Page changed after the form was loaded. Review your values and reload before saving."}, nil)
			return
		}
		draft, problems := content.PreparePageDraft(definition, values)
		if len(problems) > 0 {
			writeEditValidationForm(w, r, item, definition, values, revisionText, problems)
			return
		}
		_, err = repository.SaveDraft(r.Context(), id, revision, draft, time.Now(), actor)
		if err == nil {
			http.Redirect(w, r, "/admin/pages/"+id+"/edit", http.StatusSeeOther)
			return
		}
		var taken *content.PathTakenError
		if errors.As(err, &taken) {
			owner := &conflictOwner{ID: taken.OwnerID, Title: taken.OwnerTitle}
			writeEditProblem(w, r, http.StatusConflict, item, definition, values, revisionText,
				formProblem{Code: "path_taken", Message: "That path is already used by another Page."}, owner)
			return
		}
		if errors.Is(err, content.ErrStaleDraft) {
			writeEditProblem(w, r, http.StatusConflict, item, definition, values, revisionText,
				formProblem{Code: "stale_draft", Message: "This Page changed after the form was loaded. Review your values and reload before saving."}, nil)
			return
		}
		var pathChange *content.PathChangeUnsupportedError
		if errors.As(err, &pathChange) {
			writeEditProblem(w, r, http.StatusUnprocessableEntity, item, definition, values, revisionText,
				formProblem{Field: "path", Code: "path_change_unsupported", Message: fmt.Sprintf("Changing a published Page's path needs redirects, which arrive in a later phase. Keep %s for now. Redirect support arrives in phase 5.", pathChange.OwnedPath)}, nil)
			return
		}
		if errors.Is(err, content.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "cannot save Page", http.StatusInternalServerError)
	})
	handleAdminPost("POST /admin/pages/{id}/publish", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := pageWriteActor(w, r)
		if !ok {
			return
		}
		revisionText := r.PostForm.Get("draft_revision")
		revision, parseErr := strconv.Atoi(revisionText)
		if parseErr != nil || revision <= 0 {
			writePublishProblem(w, r, repository, http.StatusUnprocessableEntity,
				formProblem{Field: "draft_revision", Code: "invalid_revision", Message: "Reload the saved draft before publishing."}, nil)
			return
		}
		_, err := repository.Publish(r.Context(), r.PathValue("id"), revision, time.Now(), actor, content.ValidateStoredPageDraft)
		if err == nil {
			http.Redirect(w, r, "/admin/pages/"+r.PathValue("id")+"/edit?notice=published", http.StatusSeeOther)
			return
		}
		var taken *content.PathTakenError
		if errors.As(err, &taken) {
			writePublishProblem(w, r, repository, http.StatusConflict,
				formProblem{Code: "path_taken", Message: "That path is already used by another Page."}, &conflictOwner{ID: taken.OwnerID, Title: taken.OwnerTitle})
			return
		}
		if errors.Is(err, content.ErrStaleDraft) {
			writePublishProblem(w, r, repository, http.StatusConflict,
				formProblem{Code: "stale_draft", Message: "This Page changed after the form was loaded. Review the saved draft and reload before publishing."}, nil)
			return
		}
		if errors.Is(err, content.ErrPathChangeUnsupported) {
			var pathChange *content.PathChangeUnsupportedError
			errors.As(err, &pathChange)
			message := "Changing a published Page's path needs redirects, which arrive in a later phase. Redirect support arrives in phase 5."
			if pathChange != nil {
				message += " Keep " + pathChange.OwnedPath + " for now."
			}
			writePublishProblem(w, r, repository, http.StatusUnprocessableEntity,
				formProblem{Field: "path", Code: "path_change_unsupported", Message: message}, nil)
			return
		}
		var validation *content.ValidationError
		if errors.As(err, &validation) {
			item, itemErr := repository.GetItem(r.Context(), r.PathValue("id"))
			definition, configErr := activePageDefinition(r.Context(), repository)
			if itemErr != nil {
				// Corrupt persisted fields can prevent GetItem from decoding the
				// draft. Still return the validation response without rewriting it.
				item = content.Item{ID: r.PathValue("id"), Revision: revision}
			}
			if configErr != nil {
				definition = content.PageDefinition{}
			}
			problems := make([]formProblem, 0, len(validation.Problems))
			for _, problem := range validation.Problems {
				problems = append(problems, formProblem{Field: problem.Field, Code: problem.Code, Message: validationMessage(problem, definition)})
			}
			writePageForm(w, r, http.StatusUnprocessableEntity,
				editFormData(item, definition, nil, problems, strconv.Itoa(revision)))
			return
		}
		if errors.Is(err, content.ErrAlreadyPublished) {
			writePublishProblem(w, r, repository, http.StatusConflict,
				formProblem{Code: "already_published", Message: "This Page already has its first published snapshot."}, nil)
			return
		}
		if errors.Is(err, content.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "cannot publish Page", http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := repository.PublishedByPath(r.Context(), r.URL.Path)
		if errors.Is(err, content.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "cannot load page", 500)
			return
		}
		definition, err := activePageDefinition(r.Context(), repository)
		if err != nil {
			http.Error(w, "cannot load Page configuration", http.StatusInternalServerError)
			return
		}
		html, err := render.PageWithDefinition(snapshot, definition)
		if err != nil {
			http.Error(w, "cannot render page", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	methodBoundary := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (isPageSavePath(r.URL.Path) || isPagePublishPath(r.URL.Path)) && r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mux.ServeHTTP(w, r)
	})
	routes := adminGuard(methodBoundary)
	if loader, ok := repository.(SessionLoader); ok {
		routes = SessionMiddleware(loader, time.Now, routes)
	}
	protected := http.NewCrossOriginProtection().Handler(routes)
	allowed := map[string]bool{net.JoinHostPort("127.0.0.1", port): true, net.JoinHostPort("localhost", port): true, net.JoinHostPort("::1", port): true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if !allowed[r.Host] {
			http.Error(w, "unexpected Host", http.StatusBadRequest)
			return
		}
		// Reject raw aliases before ServeMux can clean or redirect them.
		if strings.Contains(r.URL.EscapedPath(), "%") || path.Clean(r.URL.Path) != r.URL.Path {
			http.NotFound(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

func isPageSavePath(path string) bool {
	const prefix = "/admin/pages/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	id := strings.TrimPrefix(path, prefix)
	return id != "" && id != "new" && !strings.Contains(id, "/")
}

func isPagePublishPath(path string) bool {
	const prefix = "/admin/pages/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	id := strings.TrimPrefix(path, prefix)
	return strings.HasSuffix(id, "/publish") && strings.Count(id, "/") == 1 && strings.TrimSuffix(id, "/publish") != "" && strings.TrimSuffix(id, "/publish") != "new"
}

func writePublishProblem(w http.ResponseWriter, r *http.Request, repository content.Repository, status int, problem formProblem, owner *conflictOwner) {
	item, err := repository.GetItem(r.Context(), r.PathValue("id"))
	if errors.Is(err, content.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "cannot load Page", http.StatusInternalServerError)
		return
	}
	definition, err := activePageDefinition(r.Context(), repository)
	if err != nil {
		http.Error(w, "cannot load Page configuration", http.StatusInternalServerError)
		return
	}
	data := editFormData(item, definition, nil, []formProblem{problem}, strconv.Itoa(item.Revision))
	data.Owner = owner
	writePageForm(w, r, status, data)
}

func parseAdminForm(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/x-www-form-urlencoded") {
		http.Error(w, "form must use application/x-www-form-urlencoded", http.StatusUnsupportedMediaType)
		return false
	}
	if r.ContentLength > adminBodyLimit {
		http.Error(w, "form is too large", http.StatusRequestEntityTooLarge)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, adminBodyLimit)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "form is too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid form", http.StatusBadRequest)
		}
		return false
	}
	return true
}

func activePageDefinition(ctx context.Context, repository content.Repository) (content.PageDefinition, error) {
	active, err := repository.ActiveConfig(ctx)
	if err != nil {
		return content.PageDefinition{}, err
	}
	document, err := config.Decode(active.Document)
	if err != nil {
		return content.PageDefinition{}, err
	}
	definition := content.PageDefinition{Revision: active.Revision}
	for _, field := range document.ContentTypes[0].Fields {
		definition.Fields = append(definition.Fields, content.FieldDefinition{
			ID: field.ID, Kind: field.Kind, Label: field.Label, HelpText: field.HelpText,
			Required: field.Required, Order: field.Order.String(),
		})
	}
	content.SortPageFields(definition.Fields)
	return definition, nil
}

func submittedValues(r *http.Request, definition content.PageDefinition) map[string]string {
	values := map[string]string{"title": r.PostForm.Get("title"), "path": r.PostForm.Get("path")}
	for _, field := range definition.Fields {
		values[field.ID] = r.PostForm.Get(field.ID)
	}
	return values
}

func newFormData(definition content.PageDefinition, values map[string]string, problems []formProblem, owner *conflictOwner) pageFormData {
	if values == nil {
		values = map[string]string{}
	}
	data := pageFormData{Heading: "New Page", Action: "/admin/pages", SubmitLabel: "Create Page", Values: values, Errors: problems, Owner: owner}
	for _, field := range definition.Fields {
		textareaValue := values[field.ID]
		if field.Kind == "long_text" && (strings.HasPrefix(textareaValue, "\n") || strings.HasPrefix(textareaValue, "\r")) {
			// The HTML parser strips one LF immediately after a textarea start
			// tag. Add a presentation-only sentinel so the control keeps the
			// user's original first newline as its value.
			textareaValue = "\n" + textareaValue
		}
		data.Fields = append(data.Fields, formField{
			ID: field.ID, Kind: field.Kind, Label: field.Label, HelpText: field.HelpText,
			Required: field.Required, Value: values[field.ID], TextareaValue: textareaValue,
		})
	}
	return data
}

func editFormData(item content.Item, definition content.PageDefinition, values map[string]string, problems []formProblem, revision string) pageFormData {
	if values == nil {
		values = map[string]string{"title": item.Title, "path": item.Path}
		for _, field := range definition.Fields {
			values[field.ID] = item.Fields[field.ID]
		}
	}
	data := newFormData(definition, values, problems, nil)
	data.Heading = "Edit Page"
	data.Action = "/admin/pages/" + item.ID
	data.PreviewURL = data.Action + "/preview"
	data.PublishURL = "/admin/pages/" + item.ID + "/publish"
	data.Revision = revision
	data.SubmitLabel = "Save changes"
	return data
}

func writePageForm(w http.ResponseWriter, r *http.Request, status int, data pageFormData) {
	data.CSRFToken = sessionCSRFToken(r)
	writeTemplateStatus(w, status, pageFormTemplate, data)
}

func writeValidationForm(w http.ResponseWriter, r *http.Request, definition content.PageDefinition, values map[string]string, problems []content.FieldError) {
	data := make([]formProblem, 0, len(problems))
	for _, problem := range problems {
		data = append(data, formProblem{Field: problem.Field, Code: problem.Code, Message: validationMessage(problem, definition)})
	}
	writePageForm(w, r, http.StatusUnprocessableEntity, newFormData(definition, values, data, nil))
}

func writeEditValidationForm(w http.ResponseWriter, r *http.Request, item content.Item, definition content.PageDefinition, values map[string]string, revision string, problems []content.FieldError) {
	data := make([]formProblem, 0, len(problems))
	for _, problem := range problems {
		data = append(data, formProblem{Field: problem.Field, Code: problem.Code, Message: validationMessage(problem, definition)})
	}
	writePageForm(w, r, http.StatusUnprocessableEntity, editFormData(item, definition, values, data, revision))
}

func writeEditProblem(w http.ResponseWriter, r *http.Request, status int, item content.Item, definition content.PageDefinition, values map[string]string, revision string, problem formProblem, owner *conflictOwner) {
	data := editFormData(item, definition, values, nil, revision)
	data.Errors = []formProblem{problem}
	data.Owner = owner
	writePageForm(w, r, status, data)
}

func validationMessage(problem content.FieldError, definition content.PageDefinition) string {
	label := problem.Field
	if problem.Field == "title" {
		label = "Title"
	} else if problem.Field == "path" {
		label = "Path"
	} else {
		for _, field := range definition.Fields {
			if field.ID == problem.Field && field.Label != "" {
				label = field.Label
				break
			}
		}
	}
	switch problem.Code {
	case "required":
		return label + " is required."
	case "too_long":
		return label + " is too long."
	case "invalid_text":
		return label + " contains a control character."
	case "path_invalid":
		return label + ": enter a valid path."
	case "path_reserved":
		return label + " starts with a reserved segment."
	default:
		return "Check this value."
	}
}

func writeTemplate(w http.ResponseWriter, page *template.Template, data any) {
	writeTemplateStatus(w, http.StatusOK, page, data)
}

func writeTemplateStatus(w http.ResponseWriter, status int, page *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := page.Execute(w, data); err != nil {
		// Rendering uses only in-memory validated values. A partial response cannot
		// be replaced with a useful error, so leave the connection's response intact.
		return
	}
}

func Server(repository content.Repository, listener net.Listener) *http.Server {
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return &http.Server{Handler: Handler(repository, port), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
}
