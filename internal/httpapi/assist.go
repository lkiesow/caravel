package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"caravel/internal/assist"
	"caravel/internal/db"
)

// AI-assisted location metadata, streamed.
//
// # Why this is a stream and not a JSON POST
//
// A run takes half a minute or more -- several searches and page reads. With a
// plain POST the browser has a spinner and nothing else for that long, which
// reads as a hang, and there is no way to say *why* it is slow. So the
// response is Server-Sent Events: progress lines as the agent works, then the
// proposal.
//
// It is a POST returning text/event-stream rather than an EventSource
// endpoint, because EventSource can only issue a GET with no body and this
// request carries the location's current metadata. The client reads it with
// fetch() and a small line parser, which also gives cancellation for free: an
// AbortController on the client surfaces here as r.Context() being done, which
// the agent checks between turns.
//
// # Progress events carry keys, not sentences
//
// The server does not know the user's language and must not be the thing
// writing UI copy. Every progress event is an i18n key plus parameters, which
// the client renders through t(). A translated string on the wire could not be
// re-rendered if the user switched language mid-run, and would put English in
// a German UI for anyone whose locale we guessed wrong.

const (
	// assistMaxPromptBytes caps the free-text prompt. Generous for a sentence
	// describing a place, small enough that this endpoint cannot be used to
	// push arbitrary text at somebody else's paid API.
	assistMaxPromptBytes = 2000
	// assistMaxFieldBytes caps each piece of existing metadata the client
	// sends back. Notes are the big one and are already short-form.
	assistMaxFieldBytes = 16000
	// assistMaxLinks caps how many existing links ride along.
	assistMaxLinks = 30
)

type assistLocationRequest struct {
	Mode   string `json:"mode"`
	Prompt string `json:"prompt"`

	// The location as it currently stands in the editor -- which is not
	// necessarily what is in the database. The editor holds unsaved changes,
	// and enriching should see what the user is looking at.
	Title    string            `json:"title"`
	Category string            `json:"category"`
	Type     string            `json:"type"`
	Notes    string            `json:"notes"`
	Address  string            `json:"address"`
	Links    []assistLinkInput `json:"links"`

	// IncludeTripContext sends the trip title and dates. Defaults to true when
	// absent, which is why it is a pointer: the dates are what make "is it
	// open in November" answerable, so they are worth sending by default --
	// but they are also the most personal thing in the payload, so the box
	// that turns them off has to actually work.
	IncludeTripContext *bool `json:"include_trip_context"`

	// Locale is the user's UI language. The client knows it; the server would
	// otherwise have to guess from Accept-Language, which is the browser's
	// preference rather than the one chosen in settings.
	Locale string `json:"locale"`
}

type assistLinkInput struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

// The wire shape of a proposal. Defined here rather than as tags on
// assist.Proposal so the transport can change without touching the domain
// package -- and so it is obvious at a glance what the client actually sees.
type assistProposalResponse struct {
	Fields  []assistFieldResponse  `json:"fields"`
	Links   []assistLinkResponse   `json:"links"`
	Lat     *float64               `json:"lat"`
	Lng     *float64               `json:"lng"`
	Sources []assistSourceResponse `json:"sources"`
	// Cover is a proposed cover photograph, or null. Nullable rather than a
	// zero-valued object because "no picture was found" is the ordinary case
	// and the client has to branch on it either way.
	Cover *assistCoverResponse `json:"cover"`
}

// assistCoverResponse is a proposed cover photograph and its provenance.
//
// Credit and licence are empty for an og:image, which carries no such
// metadata, and populated for a Wikimedia one. The client shows what is there
// rather than inventing a credit for an image that has none.
type assistCoverResponse struct {
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url"`
	// SourceURL is the page it came from. Always set -- an image with no
	// record of where it came from is a problem waiting for the day somebody
	// shares a trip.
	SourceURL string `json:"source_url"`
	Credit    string `json:"credit"`
	License   string `json:"license"`
	// From is "og" or "wikipedia", so the client can say where it came from
	// without parsing a URL.
	From string `json:"from"`
}

type assistFieldResponse struct {
	Name    string `json:"name"`
	Current string `json:"current"`
	// Proposed is what the agent suggests.
	Proposed string `json:"proposed"`
	// Overwrites tells the client this field has existing content, so it must
	// be shown as a before/after rather than as a plain suggestion. Computed
	// here rather than left to the client to infer from a non-empty Current,
	// so the rule lives in one place.
	Overwrites bool `json:"overwrites"`
}

type assistLinkResponse struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

type assistSourceResponse struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

func (s *Server) handleAssistLocation(w http.ResponseWriter, r *http.Request) {
	// 501 before anything else, matching handleGeocode: the route exists, the
	// capability is off. Before the trip lookup, so a disabled instance does
	// not leak whether a trip id exists.
	if s.Assist == nil {
		writeError(w, http.StatusNotImplemented, "the assistant is not enabled on this server")
		return
	}

	// Editor rather than viewer. Two reasons, the second load-bearing: a
	// viewer could not save the result anyway, and the request may carry the
	// trip title and dates to a third-party API, which is not a read-only
	// participant's decision to make.
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req assistLocationRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	agentReq, err := s.buildAssistRequest(r, trip, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Admission control, and the last guard before somebody's money is spent.
	// The rate limiter bounds how often runs *start*; this bounds how many are
	// alive at once, which is the thing that actually decides the worst-case
	// bill. Refused rather than queued: a request that waits behind three
	// others just times out further downstream, and the client can retry.
	release, ok := s.acquireAssistSlot()
	if !ok {
		writeErrorCode(w, http.StatusTooManyRequests, "assist_busy",
			"the assistant is busy with other requests, try again in a moment")
		return
	}
	defer release()

	s.streamAssistRun(w, r, agentReq)
}

// buildAssistRequest validates the body and adds what only the server knows:
// the trip context and the vocabulary of types already in use.
func (s *Server) buildAssistRequest(r *http.Request, trip db.Trip, req assistLocationRequest) (assist.Request, error) {
	mode := assist.Mode(strings.TrimSpace(req.Mode))
	if !mode.Valid() {
		return assist.Request{}, fmt.Errorf("mode must be %q or %q", assist.ModeEnrich, assist.ModePrompt)
	}

	prompt := strings.TrimSpace(req.Prompt)
	if len(prompt) > assistMaxPromptBytes {
		return assist.Request{}, fmt.Errorf("the prompt is too long")
	}
	// Prompt mode with no prompt is a run with nothing to look for, which
	// would spend real money discovering that.
	if mode == assist.ModePrompt && prompt == "" {
		return assist.Request{}, fmt.Errorf("a description is required to search for a new place")
	}

	for _, f := range []string{req.Title, req.Category, req.Type, req.Notes, req.Address} {
		if len(f) > assistMaxFieldBytes {
			return assist.Request{}, fmt.Errorf("the location metadata is too long")
		}
	}
	if len(req.Links) > assistMaxLinks {
		return assist.Request{}, fmt.Errorf("too many existing links")
	}

	out := assist.Request{
		Mode:   mode,
		Prompt: prompt,
		Current: assist.Location{
			Title:    strings.TrimSpace(req.Title),
			Category: strings.TrimSpace(req.Category),
			Type:     strings.TrimSpace(req.Type),
			Notes:    req.Notes,
			Address:  strings.TrimSpace(req.Address),
		},
		Locale: normaliseLocale(req.Locale),
	}
	for _, l := range req.Links {
		if u := strings.TrimSpace(l.URL); u != "" {
			out.Current.Links = append(out.Current.Links, assist.Link{URL: u, Label: strings.TrimSpace(l.Label)})
		}
	}

	// Absent means yes. The dates are what make seasonal advice possible, so
	// they are worth sending by default -- and the box that turns them off is
	// the reason this is a pointer rather than a bool.
	if req.IncludeTripContext == nil || *req.IncludeTripContext {
		out.Trip = assist.TripContext{Title: trip.Title}
		if trip.StartDate != nil {
			out.Trip.Start = *trip.StartDate
		}
		if trip.EndDate != nil {
			out.Trip.End = *trip.EndDate
		}
	}

	out.TypeVocabulary = s.tripTypeVocabulary(r, trip.ID)
	return out, nil
}

// tripTypeVocabulary collects the distinct type tags already used on this
// trip, so the model reuses one instead of inventing a near-duplicate.
//
// A failure here is not worth failing the run over: the worst case is a model
// that invents "Hotel" alongside an existing "hotel", which the user can see
// and reject in the review.
func (s *Server) tripTypeVocabulary(r *http.Request, tripID string) []string {
	items, err := s.Store.ListItemsByTrip(r.Context(), tripID, nil)
	if err != nil {
		return nil
	}
	var out []string
	for _, item := range items {
		t := strings.TrimSpace(item.Type)
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

// normaliseLocale keeps the locale to something that looks like a language
// tag. It reaches a third-party prompt, so it is not passed through unchecked.
func normaliseLocale(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > 16 {
		return ""
	}
	for _, r := range raw {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && r != '-' && r != '_' {
			return ""
		}
	}
	return raw
}

// streamAssistRun runs the agent, writing events as it goes.
func (s *Server) streamAssistRun(w http.ResponseWriter, r *http.Request, req assist.Request) {
	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		// Without flushing every event arrives at once when the handler
		// returns, which is a plain POST with extra steps. Better to say so
		// than to ship a stream that silently is not one.
		writeError(w, http.StatusInternalServerError, "streaming is not supported by this server")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	// No caching and no proxy buffering. X-Accel-Buffering is nginx-specific
	// and ignored elsewhere; without it a default nginx in front of this
	// collects the whole stream and delivers it at the end, which is the
	// classic "SSE works locally and not in production".
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	send := func(event string, payload any) {
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}
		// The SSE framing: an event name, one data line, a blank line to
		// terminate. json.Marshal cannot emit a raw newline inside a string,
		// so a single data line is always enough.
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
		flusher.Flush()
	}

	proposal, err := s.Assist.Propose(r.Context(), req, func(e assist.Event) {
		switch e.Kind {
		case assist.EventStep:
			// One finished step, for the trace the editor collects. Separate
			// from progress because the two arrive at different moments and
			// answer different questions -- see the note on EventKind.
			send("step", map[string]any{
				"key":    e.Key,
				"params": paramsOrEmpty(e.Params),
				"ms":     e.DurationMS,
				"failed": e.Failed,
			})
		case assist.EventSummary:
			send("summary", map[string]any{
				"ms":         e.Totals.DurationMS,
				"steps":      e.Totals.Steps,
				"turns":      e.Totals.Turns,
				"tool_calls": e.Totals.ToolCalls,
				"tokens":     e.Totals.Tokens,
			})
		default:
			send("progress", map[string]any{"key": e.Key, "params": paramsOrEmpty(e.Params)})
		}
	})

	if err != nil {
		// The client going away is not an error to report: there is nobody
		// left to read it, and the write would fail anyway.
		if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
			return
		}
		code := assistErrorCode(err)
		// The one place the underlying error is written down. The browser gets
		// a fixed sentence (see assistErrorMessage for why), so without this
		// the actual cause -- a wrong endpoint, a rejected key, a model name
		// the provider does not know -- existed nowhere at all.
		//
		// Error rather than debug, and unconditional: this is a failure the
		// operator has to be able to see without having first predicted it and
		// turned the level up.
		slog.Error("assist run failed", "code", code, "mode", string(req.Mode), "err", err)
		// The status line is already 200 by now, so a failure has to arrive as
		// an event rather than as a status code. The client branches on the
		// event name.
		send("error", map[string]string{
			"code":    code,
			"message": assistErrorMessage(err),
		})
		return
	}

	send("proposal", toAssistProposalResponse(proposal))
}

// paramsOrEmpty keeps params an object rather than null. The client reads them
// in exactly one place, and a null there is one more branch for no gain -- the
// same reasoning as the response slices always being [].
func paramsOrEmpty(p map[string]string) map[string]string {
	if p == nil {
		return map[string]string{}
	}
	return p
}

// assistErrorCode gives the client something stable to branch on, since the
// message is free to be reworded.
func assistErrorCode(err error) string {
	switch {
	case errors.Is(err, assist.ErrTimedOut):
		return "assist_timeout"
	case errors.Is(err, assist.ErrBudgetExhausted):
		return "assist_budget"
	default:
		return "assist_failed"
	}
}

// assistErrorMessage is deliberately not the underlying error. A provider's
// own words can carry an endpoint, a model name or an account detail, none of
// which are ours to forward to whoever is using the app. The real error is
// logged at error level where this is called, which is what the operator
// reads -- for two milestones this comment claimed that and nothing logged it.
func assistErrorMessage(err error) string {
	switch {
	case errors.Is(err, assist.ErrTimedOut):
		return "the assistant took too long and was stopped"
	case errors.Is(err, assist.ErrBudgetExhausted):
		return "the assistant reached its limit for one request"
	default:
		return "the assistant could not complete this request"
	}
}

func toAssistProposalResponse(p *assist.Proposal) assistProposalResponse {
	// Non-nil slices throughout: the client iterates them, and null is one
	// more branch at every call site for no gain.
	out := assistProposalResponse{
		Fields:  make([]assistFieldResponse, 0, len(p.Fields)),
		Links:   make([]assistLinkResponse, 0, len(p.Links)),
		Sources: make([]assistSourceResponse, 0, len(p.Sources)),
		Lat:     p.Lat,
		Lng:     p.Lng,
	}
	for _, f := range p.Fields {
		out.Fields = append(out.Fields, assistFieldResponse{
			Name: f.Name, Current: f.Current, Proposed: f.Proposed, Overwrites: f.Overwrites(),
		})
	}
	for _, l := range p.Links {
		out.Links = append(out.Links, assistLinkResponse{URL: l.URL, Label: l.Label})
	}
	for _, src := range p.Sources {
		out.Sources = append(out.Sources, assistSourceResponse{Title: src.Title, URL: src.URL})
	}
	if p.Cover != nil {
		out.Cover = &assistCoverResponse{
			URL:       p.Cover.URL,
			ThumbURL:  p.Cover.ThumbURL,
			SourceURL: p.Cover.SourceURL,
			Credit:    p.Cover.Credit,
			License:   p.Cover.Licence,
			From:      p.Cover.From,
		}
	}
	return out
}
