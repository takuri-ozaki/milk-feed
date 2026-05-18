package main

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"
)

//go:embed templates/*.html
var tmplFS embed.FS

const (
	listenAddr    = ":8080"
	dbPath        = "data.db"
	pastDisplayN  = 2
	scheduleCount = scheduleSlotCount
	datetimeLocal = "2006-01-02T15:04"
)

type viewModel struct {
	Settings           Settings
	CaregiverA         string
	CaregiverB         string
	PastFeeds          []pastFeedingView
	NextFeedAt         string // pre-filled value for the datetime-local input (now)
	Slots              []slotView
	NowDateline        string
	HasAdjustment      bool
	AdjustmentTime     string // formatted display, e.g. "5/8 19:00"
	AdjustmentReason   string
	AdjustInputDefault string // datetime-local default for the adjust form (= natural slot 1 start)
}

type pastFeedingView struct {
	ID   int64
	When string
}

type slotView struct {
	Index   int
	Range   string
	StatusA string
	StatusB string
}

type server struct {
	store *Store
	tmpl  *template.Template
	loc   *time.Location
}

func main() {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		log.Fatalf("load location: %v", err)
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	tmpl, err := template.ParseFS(tmplFS, "templates/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	s := &server{store: store, tmpl: tmpl, loc: loc}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/favicon.ico", s.handleFavicon)
	mux.HandleFunc("/settings", s.handleSettings)
	mux.HandleFunc("/assignments", s.handleAssignments)
	mux.HandleFunc("/feedings", s.handleFeedings)
	mux.HandleFunc("/feedings/delete", s.handleFeedingsDelete)
	mux.HandleFunc("/adjustment", s.handleAdjustment)
	mux.HandleFunc("/adjustment/delete", s.handleAdjustmentDelete)

	log.Printf("milking server listening on http://localhost%s", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	settings, err := s.store.GetSettings()
	if err != nil {
		httpErr(w, err)
		return
	}

	now := time.Now().In(s.loc)

	anchor, hasAnchor, err := s.store.LatestFeedingTime()
	if err != nil {
		httpErr(w, err)
		return
	}
	if !hasAnchor {
		anchor = time.Now()
	}

	asn, err := s.store.AllAssignments()
	if err != nil {
		httpErr(w, err)
		return
	}

	adj, hasAdj, err := s.store.GetNextAdjustment()
	if err != nil {
		httpErr(w, err)
		return
	}
	var adjPtr *time.Time
	if hasAdj {
		t := adj.Target
		adjPtr = &t
	}

	slots := BuildSchedule(anchor, settings.IntervalMinMinutes, settings.IntervalMaxMinutes, scheduleCount, asn, adjPtr)
	slotViews := make([]slotView, 0, len(slots))
	for _, sl := range slots {
		slotViews = append(slotViews, slotView{
			Index:   sl.Index,
			Range:   FormatSlotRange(sl, s.loc, now),
			StatusA: sl.StatusA,
			StatusB: sl.StatusB,
		})
	}

	past, err := s.store.RecentFeedings(pastDisplayN)
	if err != nil {
		httpErr(w, err)
		return
	}
	pastViews := make([]pastFeedingView, 0, len(past))
	for _, f := range past {
		pastViews = append(pastViews, pastFeedingView{
			ID:   f.ID,
			When: formatPoint(f.FedAt.In(s.loc), now),
		})
	}

	naturalNext := anchor.Add(time.Duration(settings.IntervalMinMinutes) * time.Minute).In(s.loc)
	adjustInputDefault := naturalNext.Format(datetimeLocal)
	if hasAdj {
		adjustInputDefault = adj.Target.In(s.loc).Format(datetimeLocal)
	}

	vm := viewModel{
		Settings:           settings,
		CaregiverA:         CaregiverAName,
		CaregiverB:         CaregiverBName,
		PastFeeds:          pastViews,
		NextFeedAt:         now.Format(datetimeLocal),
		Slots:              slotViews,
		NowDateline:        now.Format("2006-01-02 (Mon) 15:04"),
		HasAdjustment:      hasAdj,
		AdjustInputDefault: adjustInputDefault,
	}
	if hasAdj {
		vm.AdjustmentTime = formatPoint(adj.Target.In(s.loc), now)
		vm.AdjustmentReason = adj.Reason
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", vm); err != nil {
		log.Printf("template execute: %v", err)
	}
}

func (s *server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/favicon.ico")
}

func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		httpErr(w, err)
		return
	}
	min, err := strconv.Atoi(r.FormValue("interval_min_minutes"))
	if err != nil {
		http.Error(w, "interval_min_minutes invalid", http.StatusBadRequest)
		return
	}
	max, err := strconv.Atoi(r.FormValue("interval_max_minutes"))
	if err != nil {
		http.Error(w, "interval_max_minutes invalid", http.StatusBadRequest)
		return
	}
	st := Settings{
		IntervalMinMinutes: min,
		IntervalMaxMinutes: max,
	}
	if err := s.store.UpdateSettings(st); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleAssignments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		httpErr(w, err)
		return
	}
	idx, err := strconv.Atoi(r.FormValue("slot_index"))
	if err != nil || idx < 1 {
		http.Error(w, "slot_index invalid", http.StatusBadRequest)
		return
	}
	caregiver := r.FormValue("caregiver")
	if caregiver != "a" && caregiver != "b" {
		http.Error(w, "caregiver invalid", http.StatusBadRequest)
		return
	}
	status := r.FormValue("status")
	switch status {
	case "o", "t", "x", "none", "":
	default:
		http.Error(w, "status invalid", http.StatusBadRequest)
		return
	}
	if err := s.store.SetAssignment(idx, caregiver, status); err != nil {
		httpErr(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleFeedings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		httpErr(w, err)
		return
	}
	raw := r.FormValue("fed_at")
	var t time.Time
	if raw == "" {
		t = time.Now()
	} else {
		parsed, err := time.ParseInLocation(datetimeLocal, raw, s.loc)
		if err != nil {
			http.Error(w, "fed_at invalid", http.StatusBadRequest)
			return
		}
		t = parsed
	}
	if err := s.store.AddFeedingAndShift(t); err != nil {
		httpErr(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleFeedingsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		httpErr(w, err)
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id invalid", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteFeeding(id); err != nil {
		httpErr(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleAdjustment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		httpErr(w, err)
		return
	}
	raw := r.FormValue("target")
	if raw == "" {
		http.Error(w, "target required", http.StatusBadRequest)
		return
	}
	target, err := time.ParseInLocation(datetimeLocal, raw, s.loc)
	if err != nil {
		http.Error(w, "target invalid", http.StatusBadRequest)
		return
	}
	reason := r.FormValue("reason")
	if err := s.store.SetNextAdjustment(target, reason); err != nil {
		httpErr(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleAdjustmentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.store.ClearNextAdjustment(); err != nil {
		httpErr(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func httpErr(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
