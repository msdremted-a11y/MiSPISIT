// Веб-часть «Системного монитора на базе eBPF + Qt».
// Запускает HTTP-сервер, связывает адреса со страницами и тремя функциями.
package main

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/msdremted-a11y/MiSPISIT/internal/actions"
	"github.com/msdremted-a11y/MiSPISIT/internal/probes"
	"github.com/msdremted-a11y/MiSPISIT/internal/procstat"
	"github.com/msdremted-a11y/MiSPISIT/internal/storage"
)

const (
	product = "Системный монитор на базе eBPF + Qt"
	version = "0.1.0"
)

type server struct {
	svc     *actions.Service
	pages   map[string]*template.Template
	grafana string // адрес панели Grafana для ссылок в меню
}

// page — общие данные для всех шаблонов.
type page struct {
	Product, Version, Active, Grafana string
	Data                              any
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dir := env("APP_DIR", ".")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("не задана переменная DATABASE_URL (адрес базы PostgreSQL)")
	}
	store, err := storage.OpenPostgres(dbURL, 30)
	if err != nil {
		log.Fatalf("не удалось подключиться к базе: %v", err)
	}

	funcs := template.FuncMap{
		"dt": func(t time.Time) string { return t.UTC().Format("02.01.2006 15:04:05") + " UTC" },
		"source": func(s string) string {
			if s == "proc" {
				return "автоматически из /proc"
			}
			return "вручную"
		},
	}
	pages := map[string]*template.Template{}
	for _, name := range []string{"index", "list", "form", "card", "rejected", "notifications",
		"probes", "probeform", "probecard", "notfound", "error"} {
		pages[name] = template.Must(template.New("layout.html").Funcs(funcs).ParseFiles(
			filepath.Join(dir, "templates", "layout.html"),
			filepath.Join(dir, "templates", name+".html"),
		))
	}

	s := &server{
		svc: &actions.Service{
			Store:   store,
			Measure: func() (procstat.Metrics, error) { return procstat.Measure(500 * time.Millisecond) },
			Now:     time.Now,
		},
		pages:   pages,
		grafana: env("GRAFANA_URL", "http://localhost:3000"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /snapshots", s.list)
	mux.HandleFunc("GET /snapshots/new", s.form)
	mux.HandleFunc("POST /snapshots", s.create)
	mux.HandleFunc("GET /snapshots/{id}", s.card)
	mux.HandleFunc("GET /rejected", s.rejected)
	mux.HandleFunc("GET /notifications", s.notifications)
	mux.HandleFunc("POST /notifications/{id}/read", s.readNotification)
	mux.HandleFunc("POST /notifications/read-all", s.readAllNotifications)
	mux.HandleFunc("GET /probes", s.probeList)
	mux.HandleFunc("GET /probes/new", s.probeForm)
	mux.HandleFunc("POST /probes", s.probeCreate)
	mux.HandleFunc("GET /probes/{id}", s.probeCard)
	mux.HandleFunc("POST /probes/{id}/pause", s.probeSetStatus("paused"))
	mux.HandleFunc("POST /probes/{id}/resume", s.probeSetStatus("active"))
	mux.HandleFunc("POST /probes/{id}/delete", s.probeDelete)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(dir, "static")))))
	mux.HandleFunc("/", s.notFound)

	addr := ":" + env("PORT", "8080")
	log.Printf("%s %s слушает %s", product, version, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *server) render(w http.ResponseWriter, status int, name, active string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.pages[name].Execute(w, page{product, version, active, s.grafana, data}); err != nil {
		log.Printf("шаблон %s: %v", name, err)
	}
}

// fail показывает страницу ошибки, если база недоступна.
func (s *server) fail(w http.ResponseWriter, err error) {
	log.Printf("ошибка базы: %v", err)
	s.render(w, http.StatusInternalServerError, "error", "", "База данных сейчас недоступна. Попробуйте обновить страницу через несколько секунд.")
}

// GET / — главная.
func (s *server) index(w http.ResponseWriter, r *http.Request) {
	snaps, rej, unread, err := s.svc.Counts()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "index", "home", map[string]int{"Snapshots": snaps, "Rejected": rej, "Unread": unread})
}

// GET /snapshots — функция 2: список снимков.
func (s *server) list(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.List()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "list", "list", list)
}

// GET /snapshots/new — форма функции 1.
func (s *server) form(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "form", "new", nil)
}

// POST /snapshots — функция 1; при ошибке — функция 3.
func (s *server) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "неверная форма", http.StatusBadRequest)
		return
	}
	sn, rej, err := s.svc.Create(actions.Input{
		Mode:        r.FormValue("mode"),
		Author:      r.FormValue("author"),
		CPUPercent:  r.FormValue("cpu_percent"),
		IOLatencyMs: r.FormValue("io_latency_ms"),
		IOPS:        r.FormValue("iops"),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	if rej != nil {
		http.Redirect(w, r, "/rejected?id="+strconv.Itoa(rej.ID), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/snapshots/"+strconv.Itoa(sn.ID)+"?saved=1", http.StatusSeeOther)
}

// GET /snapshots/{id} — функция 2: карточка снимка.
func (s *server) card(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err == nil {
		var sn storage.Snapshot
		sn, err = s.svc.Get(id)
		if err == nil {
			s.render(w, http.StatusOK, "card", "list", map[string]any{
				"S": sn, "Saved": r.URL.Query().Get("saved") == "1",
			})
			return
		}
		if !errors.Is(err, storage.ErrNotFound) {
			s.fail(w, err)
			return
		}
	}
	s.render(w, http.StatusNotFound, "notfound", "", "Снимка с номером «"+r.PathValue("id")+"» нет.")
}

// GET /rejected — функция 3: страница отказа.
func (s *server) rejected(w http.ResponseWriter, r *http.Request) {
	all, err := s.svc.Rejections()
	if err != nil {
		s.fail(w, err)
		return
	}
	var current *storage.Rejection
	if id, err := strconv.Atoi(r.URL.Query().Get("id")); err == nil {
		for i := range all {
			if all[i].ID == id {
				current = &all[i]
			}
		}
	}
	if current == nil && len(all) > 0 {
		current = &all[0]
	}
	s.render(w, http.StatusOK, "rejected", "rejected", map[string]any{"Current": current, "All": all})
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, "notfound", "", "Страницы «"+r.URL.Path+"» нет.")
}

// ---------- Конструктор eBPF-пробников (курсовая) ----------

// probeView — строка списка пробников.
type probeView struct {
	P       storage.Probe
	HasLast bool
	Last    float64
	Unit    string
	EventT  string
}

// GET /probes — список пробников с последними замерами.
func (s *server) probeList(w http.ResponseWriter, r *http.Request) {
	all, last, has, err := s.svc.Store.ListProbes()
	if err != nil {
		s.fail(w, err)
		return
	}
	items := make([]probeView, 0, len(all))
	for _, p := range all {
		items = append(items, probeView{
			P: p, HasLast: has[p.ID], Last: last[p.ID],
			Unit: probes.Aggs[p.Agg].Unit, EventT: probes.Events[p.Event].Title,
		})
	}
	s.render(w, http.StatusOK, "probes", "probes", items)
}

// probeFormData — данные формы конструктора (и при первом показе, и при ошибке).
type probeFormData struct {
	Err      string
	Name     string
	Event    string
	Process  string
	Agg      string
	Interval int
}

// GET /probes/new — форма конструктора.
func (s *server) probeForm(w http.ResponseWriter, r *http.Request) {
	s.renderProbeForm(w, probeFormData{Event: "syscall_read", Agg: "count", Interval: 10})
}

func (s *server) renderProbeForm(w http.ResponseWriter, d probeFormData) {
	allowed := map[string][]probes.Agg{}
	for _, code := range probes.EventCodes() {
		allowed[code] = probes.AllowedAggs(code)
	}
	s.render(w, http.StatusOK, "probeform", "probes", map[string]any{
		"F": d, "Events": probes.Events, "Order": probes.EventCodes(), "Allowed": allowed,
	})
}

// POST /probes — собрать пробник из параметров формы.
func (s *server) probeCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "неверная форма", http.StatusBadRequest)
		return
	}
	interval, _ := strconv.Atoi(r.FormValue("interval_s"))
	d := probeFormData{
		Name: r.FormValue("name"), Event: r.FormValue("event"),
		Process: r.FormValue("process"), Agg: r.FormValue("agg"), Interval: interval,
	}
	pr, msg := probes.Build(d.Name, d.Event, d.Process, d.Agg, d.Interval)
	if msg != "" {
		d.Err = msg
		s.renderProbeForm(w, d)
		return
	}
	saved, err := s.svc.Store.AddProbe(pr)
	if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/probes/"+strconv.Itoa(saved.ID)+"?saved=1", http.StatusSeeOther)
}

// GET /probes/{id} — карточка пробника: параметры, DSL, bpftrace, замеры.
func (s *server) probeCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err == nil {
		var pr storage.Probe
		pr, err = s.svc.Store.GetProbe(id)
		if err == nil {
			samples, serr := s.svc.Store.ListProbeSamples(id, 50)
			if serr != nil {
				s.fail(w, serr)
				return
			}
			s.render(w, http.StatusOK, "probecard", "probes", map[string]any{
				"P": pr, "Samples": samples, "Saved": r.URL.Query().Get("saved") == "1",
				"Unit": probes.Aggs[pr.Agg].Unit, "EventT": probes.Events[pr.Event].Title,
				"AggT": probes.Aggs[pr.Agg].Title,
			})
			return
		}
		if !errors.Is(err, storage.ErrNotFound) {
			s.fail(w, err)
			return
		}
	}
	s.render(w, http.StatusNotFound, "notfound", "", "Пробника с номером «"+r.PathValue("id")+"» нет.")
}

// probeSetStatus возвращает обработчик паузы/возобновления пробника.
func (s *server) probeSetStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			s.render(w, http.StatusNotFound, "notfound", "", "Пробника «"+r.PathValue("id")+"» нет.")
			return
		}
		if err := s.svc.Store.SetProbeStatus(id, status); err != nil && !errors.Is(err, storage.ErrNotFound) {
			s.fail(w, err)
			return
		}
		http.Redirect(w, r, "/probes/"+strconv.Itoa(id), http.StatusSeeOther)
	}
}

// POST /probes/{id}/delete — удалить пробник вместе с замерами.
func (s *server) probeDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		s.render(w, http.StatusNotFound, "notfound", "", "Пробника «"+r.PathValue("id")+"» нет.")
		return
	}
	if err := s.svc.Store.DeleteProbe(id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/probes", http.StatusSeeOther)
}

// noteView — строка таблицы уведомлений.
type noteView struct {
	N   storage.Notification
	New bool
}

// GET /notifications — экран ЛР5: сообщения о сохранённых снимках.
func (s *server) notifications(w http.ResponseWriter, r *http.Request) {
	all, err := s.svc.Notifications()
	if err != nil {
		s.fail(w, err)
		return
	}
	items := make([]noteView, 0, len(all))
	unread := 0
	for _, n := range all {
		isNew := n.Status == "new"
		if isNew {
			unread++
		}
		items = append(items, noteView{N: n, New: isNew})
	}
	s.render(w, http.StatusOK, "notifications", "notifications", map[string]any{"Items": items, "Unread": unread})
}

// POST /notifications/{id}/read — отметить одно сообщение прочитанным.
func (s *server) readNotification(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		s.render(w, http.StatusNotFound, "notfound", "", "Уведомления «"+r.PathValue("id")+"» нет.")
		return
	}
	if err := s.svc.MarkRead(id); err != nil && !errors.Is(err, storage.ErrNotFound) {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}

// POST /notifications/read-all — отметить прочитанными все сообщения.
func (s *server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.MarkAllRead(); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}
