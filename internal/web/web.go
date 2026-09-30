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
var savedPageTemplate = template.Must(template.ParseFS(adminTemplates, "admin_saved.html"))

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
	Heading string
	Fields  []formField
	Values  map[string]string
	Errors  []formProblem
	Owner   *conflictOwner
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})
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
		writeTemplate(w, pageFormTemplate, newFormData(definition, nil, nil, nil))
	})
	mux.HandleFunc("POST /admin/pages", func(w http.ResponseWriter, r *http.Request) {
		if !parseAdminForm(w, r) {
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
			writeValidationForm(w, definition, values, problems)
			return
		}
		id, err := repository.CreateItem(r.Context(), draft, time.Now())
		if err != nil {
			var taken *content.PathTakenError
			if errors.As(err, &taken) {
				owner := &conflictOwner{ID: taken.OwnerID, Title: taken.OwnerTitle}
				data := newFormData(definition, values, nil, owner)
				writeTemplateStatus(w, http.StatusConflict, pageFormTemplate, data)
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
		writeTemplate(w, savedPageTemplate, savedPageData(item, definition))
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
		html, err := render.Page(snapshot)
		if err != nil {
			http.Error(w, "cannot render page", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	protected := http.NewCrossOriginProtection().Handler(mux)
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
	data := pageFormData{Heading: "New Page", Values: values, Errors: problems, Owner: owner}
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

func writeValidationForm(w http.ResponseWriter, definition content.PageDefinition, values map[string]string, problems []content.FieldError) {
	data := make([]formProblem, 0, len(problems))
	for _, problem := range problems {
		data = append(data, formProblem{Field: problem.Field, Code: problem.Code, Message: validationMessage(problem, definition)})
	}
	writeTemplateStatus(w, http.StatusUnprocessableEntity, pageFormTemplate, newFormData(definition, values, data, nil))
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

type savedPageDataValue struct {
	Item   content.Item
	Fields []formField
}

func savedPageData(item content.Item, definition content.PageDefinition) savedPageDataValue {
	data := savedPageDataValue{Item: item}
	for _, field := range definition.Fields {
		data.Fields = append(data.Fields, formField{
			ID: field.ID, Kind: field.Kind, Label: field.Label, HelpText: field.HelpText,
			Required: field.Required, Value: item.Fields[field.ID],
		})
	}
	return data
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
