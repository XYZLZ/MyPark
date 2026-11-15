package simulator

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// ControlTokenHeader carries the control API's shared secret (M2C-46).
const ControlTokenHeader = "X-Simulator-Token"

const (
	// maxControlBody caps a control request body; the largest is a few
	// dozen bytes.
	maxControlBody = 4 << 10
	// maxEvents is the most entries one /control/events answer holds.
	maxEvents = 500
)

// control serves the control API (design §13.3).
type control struct {
	run    *Runner
	events *EventLog
	token  [sha256.Size]byte
	hasTok bool
	log    *slog.Logger
	v      *validation.Validator
}

type controlRoute struct {
	pattern string
	handle  func(*control, http.ResponseWriter, *http.Request)
}

// controlRoutes is the one table NewControl serves and ControlRoutes lists,
// so the OpenAPI parity test checks what is actually registered.
var controlRoutes = []controlRoute{
	{"GET /control/status", (*control).status},
	{"POST /control/scenarios/start", (*control).start},
	{"POST /control/scenarios/stop", (*control).stop},
	{"POST /control/cars", (*control).inject},
	{"POST /control/cars/{car_id}/leave", (*control).leave},
	{"POST /control/sensors/{code}/fault", (*control).fault},
	{"GET /control/events", (*control).listEvents},
}

// ControlRoutes returns the patterns NewControl serves.
func ControlRoutes() []string {
	out := make([]string, len(controlRoutes))
	for i, rt := range controlRoutes {
		out[i] = rt.pattern
	}
	return out
}

// NewControl returns the control API over r and its event log. Every request
// under it, a route or not, must carry token in X-Simulator-Token: the header
// is hashed and compared in constant time, so neither the value nor its
// length leaks through timing, and a missing or wrong token gets the same 401
// simulator.unauthorized. An empty token rejects everything. The token is
// never logged.
func NewControl(r *Runner, log *EventLog, token secret.String, l *slog.Logger) http.Handler {
	c := &control{
		run: r, events: log, token: sha256.Sum256([]byte(token.Reveal())), hasTok: token.Reveal() != "",
		log: l, v: validation.New(),
	}
	mux := http.NewServeMux()
	for _, rt := range controlRoutes {
		mux.HandleFunc(rt.pattern, func(w http.ResponseWriter, req *http.Request) { rt.handle(c, w, req) })
	}
	return c.guard(middleware.JSONErrors(mux))
}

func (c *control) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := sha256.Sum256([]byte(r.Header.Get(ControlTokenHeader)))
		if subtle.ConstantTimeCompare(got[:], c.token[:]) != 1 || !c.hasTok {
			httpx.WriteError(w, r, c.log, apperr.Unauthenticated(errcodes.SimulatorUnauthorized))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// decode reads a strict JSON body into out (PF7): 415 for another content
// type, 413 past maxControlBody, 400 for malformed JSON or a wrong JSON type,
// 422 unknown_field for an unknown, duplicate or case-variant key, then the
// validate tags.
func (c *control) decode(w http.ResponseWriter, r *http.Request, out any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return apperr.New(apperr.KindUnsupportedMedia, errcodes.HTTPUnsupportedMediaType)
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxControlBody))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return apperr.New(apperr.KindTooLarge, errcodes.HTTPPayloadTooLarge)
		}
		return apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON)
	}
	if err := httpx.DecodeStrict(raw, out); err != nil {
		return err
	}
	return httpx.Validate(c.v, out)
}

func (c *control) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, c.log, controlError(err))
}

// controlError maps runner errors to catalog codes. Runner messages quote
// request values, so none is sent.
func controlError(err error) error {
	switch {
	case errors.Is(err, ErrAlreadyRunning):
		return apperr.Conflict(errcodes.SimulatorAlreadyRunning)
	case errors.Is(err, ErrNotRunning):
		return apperr.Conflict(errcodes.SimulatorNotRunning)
	case errors.Is(err, ErrScenarioUnavailable):
		return apperr.Invalid(errcodes.SimulatorScenarioUnavailable)
	case errors.Is(err, ErrCarNotFound):
		return apperr.NotFound(errcodes.SimulatorCarNotFound)
	case errors.Is(err, ErrUnknownSpace):
		return httpx.InvalidFields(validation.FieldError{Field: "code", Rule: "known_space"})
	case errors.Is(err, ErrInvalidParams):
		return httpx.InvalidFields()
	}
	return err
}

func (c *control) status(w http.ResponseWriter, r *http.Request) {
	response.OK(w, r, http.StatusOK, c.run.Status())
}

type startRequest struct {
	Scenario Scenario   `json:"scenario" validate:"required,oneof=idle weekday weekend_peak fill_to_full sensor_drift reservation_arrivals no_show"`
	Seed     *int64     `json:"seed" validate:"omitnil,min=0"`
	Speed    *int       `json:"speed" validate:"omitnil,min=1,max=120"`
	StartAt  *time.Time `json:"start_at"`
}

// start begins a scenario. An omitted speed is DefaultSpeed. An omitted seed
// is drawn at random (63 bits, the contract's int64 range) and reported in
// the status, so the run can be replayed with that seed. The run is bound to
// the runner's process context, not to this request.
func (c *control) start(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := c.decode(w, r, &req); err != nil {
		c.fail(w, r, err)
		return
	}
	p := StartParams{Scenario: req.Scenario, Speed: DefaultSpeed}
	if req.Speed != nil {
		p.Speed = *req.Speed
	}
	if req.Seed != nil {
		p.Seed = uint64(*req.Seed)
	} else {
		p.Seed = rand.Uint64() >> 1
	}
	if req.StartAt != nil {
		p.StartAt = *req.StartAt
	}
	st, err := c.run.Start(c.run.runCtx(), p)
	if err != nil {
		c.fail(w, r, err)
		return
	}
	response.OK(w, r, http.StatusAccepted, st)
}

type stopRequest struct {
	Drain bool `json:"drain"`
}

// stop ends the run. A drain can take up to drainTimeout, so it runs in the
// background behind the 202; a hard stop is quick and answers once done.
func (c *control) stop(w http.ResponseWriter, r *http.Request) {
	var req stopRequest
	if err := c.decode(w, r, &req); err != nil {
		c.fail(w, r, err)
		return
	}
	stop := c.run.Stop
	if req.Drain {
		stop = c.run.StopAsync
	}
	st, err := stop(req.Drain)
	if err != nil {
		c.fail(w, r, err)
		return
	}
	response.OK(w, r, http.StatusAccepted, st)
}

type injectRequest struct {
	Credential   CredentialKind `json:"credential" validate:"required,oneof=ticket employee_card"`
	Card         *string        `json:"card"`
	Hold         bool           `json:"hold"`
	DwellMinutes *int           `json:"dwell_minutes" validate:"omitnil,min=1,max=600"`
}

func (c *control) inject(w http.ResponseWriter, r *http.Request) {
	var req injectRequest
	if err := c.decode(w, r, &req); err != nil {
		c.fail(w, r, err)
		return
	}
	p := InjectParams{Cred: req.Credential, Hold: req.Hold}
	if req.Card != nil {
		switch {
		case req.Credential != CredEmployeeCard:
			c.fail(w, r, httpx.InvalidFields(validation.FieldError{Field: "card", Rule: "excluded_unless", Param: "credential employee_card"}))
			return
		case !cardPattern.MatchString(*req.Card):
			c.fail(w, r, httpx.InvalidFields(validation.FieldError{Field: "card", Rule: "pattern", Param: cardPattern.String()}))
			return
		}
		p.Card = *req.Card
	}
	if req.DwellMinutes != nil {
		p.DwellMinutes = *req.DwellMinutes
	}
	id, err := c.run.Inject(p)
	if err != nil {
		c.fail(w, r, err)
		return
	}
	response.OK(w, r, http.StatusAccepted, struct {
		CarID string `json:"car_id"`
	}{id})
}

type leaveRequest struct {
	LoseTicket bool `json:"lose_ticket"`
}

func (c *control) leave(w http.ResponseWriter, r *http.Request) {
	var req leaveRequest
	if err := c.decode(w, r, &req); err != nil {
		c.fail(w, r, err)
		return
	}
	if err := c.run.Leave(r.PathValue("car_id"), req.LoseTicket); err != nil {
		c.fail(w, r, err)
		return
	}
	response.OK(w, r, http.StatusAccepted, struct{}{})
}

type faultRequest struct {
	Fault Fault `json:"fault" validate:"required,oneof=stuck_occupied stuck_free silent none"`
}

func (c *control) fault(w http.ResponseWriter, r *http.Request) {
	var req faultRequest
	if err := c.decode(w, r, &req); err != nil {
		c.fail(w, r, err)
		return
	}
	if err := c.run.SetFault(r.PathValue("code"), req.Fault); err != nil {
		c.fail(w, r, err)
		return
	}
	response.NoContent(w)
}

// listEvents returns up to maxEvents log entries after the `after` sequence
// number, oldest first; a client far behind polls again from the last seq.
func (c *control) listEvents(w http.ResponseWriter, r *http.Request) {
	var after int64
	if q := r.URL.Query(); q.Has("after") {
		n, err := strconv.ParseInt(q.Get("after"), 10, 64)
		switch {
		case err != nil:
			c.fail(w, r, httpx.InvalidFields(validation.FieldError{Field: "after", Rule: "integer"}))
			return
		case n < 0:
			c.fail(w, r, httpx.InvalidFields(validation.FieldError{Field: "after", Rule: "min", Param: "0"}))
			return
		}
		after = n
	}
	entries := c.events.After(after)
	response.OK(w, r, http.StatusOK, entries[:min(len(entries), maxEvents)])
}
