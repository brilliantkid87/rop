// Package httpapi implements the ROP/HTTP binding v0.1 (Master Prompt §20,
// §28; docs/capability-model.md). Only this package may import HTTP: Core
// semantics stay transport-neutral (invariant I-17). Transport success never
// implies semantic success; semantic outcomes travel in the body (§28).
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/clock"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/planner"
	"github.com/brilliantkid87/rop/internal/reversal"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/internal/verification"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// Config wires the binding. Capability booleans are the authority for what
// this server offers (capability-model.md §3); a disabled capability returns
// 501 + capability-unavailable (OQ-11 tentative decision).
type Config struct {
	ProviderID   string
	Clock        clock.Clock
	Store        *store.Store
	Registry     *operation.Registry
	Authorizer   authz.Authorizer
	Reversal     *reversal.Service
	Planner      *planner.Service
	Verification *verification.Service
	Capabilities rop.Capabilities
	Principal    authz.Principal
	Scope        string
	Logger       *slog.Logger
}

// Handler builds the ROP HTTP mux (mounted by the host application).
func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()
	h := &handler{cfg: cfg}
	mux.HandleFunc("GET /.well-known/rop", h.discovery)
	mux.HandleFunc("GET /.well-known/rop/actions/{actionId}", h.actionStatus)
	mux.HandleFunc("POST /.well-known/rop/actions/{actionId}/plan-reversal", h.planReversal)
	mux.HandleFunc("POST /.well-known/rop/actions/{actionId}/reverse", h.reverse)
	mux.HandleFunc("GET /.well-known/rop/actions/{actionId}/verification", h.verification)
	return mux
}

type handler struct{ cfg Config }

func (h *handler) log() *slog.Logger {
	if h.cfg.Logger != nil {
		return h.cfg.Logger
	}
	return slog.Default()
}

func (h *handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// writeProblem emits an RFC 9457 problem+json response (Master Prompt §63).
// Detail is sanitized at the binding boundary: echoed client input is
// truncated and stripped of control characters (M6 security second pass).
func (h *handler) writeProblem(w http.ResponseWriter, e *roperr.Error, status int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   e.ProblemType,
		"title":  problemTitle(e.ProblemType),
		"status": status,
		"detail": sanitizeDetail(e.Detail),
	})
}

// sanitizeDetail bounds and cleans human-readable detail text. Semantic
// content lives in the problem type; detail is for humans only (§63) and
// must never become a reflection channel for unbounded or control-laden
// client input.
func sanitizeDetail(detail string) string {
	if len(detail) > 256 {
		detail = detail[:256]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, detail)
}

func problemTitle(t string) string {
	switch t {
	case rop.ProblemActionNotFound:
		return "action not found"
	case rop.ProblemReversalExpired:
		return "reversal expired"
	case rop.ProblemIrreversible:
		return "irreversible action"
	case rop.ProblemReversalConflict:
		return "reversal conflict"
	case rop.ProblemAlreadyInProgress:
		return "reversal already in progress"
	case rop.ProblemAuthorizationDenied:
		return "authorization denied"
	case rop.ProblemPreconditionFailed:
		return "precondition failed"
	case rop.ProblemVerificationFailed:
		return "verification failed"
	case rop.ProblemCapabilityUnavailable:
		return "capability unavailable"
	case rop.ProblemIdempotencyConflict:
		return "idempotency key conflict"
	case rop.ProblemDependencyExists:
		return "dependency exists"
	default:
		return "rop problem"
	}
}

// statusForProblem maps semantic problem types to transport status codes.
// The mapping lives here, in the binding, not in Core (invariant I-17).
func statusForProblem(t string) int {
	switch t {
	case rop.ProblemActionNotFound:
		return http.StatusNotFound
	case rop.ProblemReversalExpired:
		return http.StatusGone
	case rop.ProblemIrreversible:
		return http.StatusUnprocessableEntity
	case rop.ProblemReversalConflict:
		return http.StatusConflict
	case rop.ProblemAlreadyInProgress:
		return http.StatusConflict
	case rop.ProblemIdempotencyConflict:
		return http.StatusConflict
	case rop.ProblemDependencyExists:
		return http.StatusConflict
	case rop.ProblemAuthorizationDenied:
		return http.StatusForbidden
	case rop.ProblemPreconditionFailed:
		return http.StatusUnprocessableEntity
	case rop.ProblemVerificationFailed:
		return http.StatusUnprocessableEntity
	case rop.ProblemCapabilityUnavailable:
		// 501 with non-retryable semantics (OQ-11 tentative decision): a
		// known capability that this provider does not offer.
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

func (h *handler) fail(w http.ResponseWriter, err error) {
	if e := roperr.From(err); e != nil {
		h.writeProblem(w, e, statusForProblem(e.ProblemType))
		return
	}
	h.log().Error("internal error", "err", err)
	h.writeProblem(w, roperr.New("urn:rop:problem:internal-error", "internal error"),
		http.StatusInternalServerError)
}

// requireCapability implements capability-model.md §3: requests to a
// non-advertised capability's endpoint fail explicitly, not silently (OQ-11).
func (h *handler) requireCapability(w http.ResponseWriter, advertised bool) bool {
	if advertised {
		return true
	}
	h.writeProblem(w, roperr.New(rop.ProblemCapabilityUnavailable,
		"this provider does not advertise this optional capability (docs/capability-model.md §3)"),
		http.StatusNotImplemented)
	return false
}

func (h *handler) discovery(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, rop.Discovery{
		Protocol:     "rop",
		Versions:     []string{"0.1"},
		Binding:      "http",
		Capabilities: h.cfg.Capabilities,
	})
}

// actionStatus returns the current receipt-shaped Action document: immutable
// identity fields with the live status and eligibility evaluated at server
// time (capability-model.md §2.4; eligibility per OQ-13 option (b): state +
// expiresAt, no implied invocability).
func (h *handler) actionStatus(w http.ResponseWriter, r *http.Request) {
	actionID := r.PathValue("actionId")
	if _, err := h.cfg.Store.SweepExpiry(r.Context(), h.cfg.Store.DB(), h.cfg.Clock.Now()); err != nil {
		h.fail(w, err)
		return
	}
	a, ok, err := h.cfg.Store.GetAction(r.Context(), h.cfg.Store.DB(), h.cfg.Scope, actionID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if !ok {
		h.fail(w, roperr.New(rop.ProblemActionNotFound, "no action %s in scope %s", actionID, h.cfg.Scope))
		return
	}
	// Residue is first-class on the public receipt (Master Prompt §45):
	// append-style records are aggregated, deduplicated by description.
	records, err := h.cfg.Store.ResidueForAction(r.Context(), h.cfg.Store.DB(), actionID)
	if err != nil {
		h.fail(w, err)
		return
	}
	var residue []rop.Residue
	seen := map[string]bool{}
	for _, rec := range records {
		for _, item := range rec.Residue {
			if !seen[item.Description] {
				seen[item.Description] = true
				residue = append(residue, item)
			}
		}
	}
	h.writeJSON(w, http.StatusOK, ReceiptFor(h.cfg.ProviderID, a, residue))
}

// ReceiptFor renders the public receipt for an Action (Master Prompt §12).
// Private reversal material is structurally absent from this type (I-14).
func ReceiptFor(providerID string, a store.ActionRow, residue []rop.Residue) rop.Receipt {
	rec := rop.Receipt{
		ActionID:      a.ActionID,
		ProviderID:    providerID,
		OperationID:   a.OperationID,
		CreatedAt:     a.CreatedAt,
		ResourceRef:   rop.ResourceRef{ResourceType: a.ResourceType, ResourceID: a.ResourceID},
		Reversibility: rop.Reversibility(a.Reversibility),
		Guarantee:     rop.Guarantee(a.Guarantee),
		Status:        rop.Status(a.Status),
		ExpiresAt:     a.ExpiresAt,
	}
	if len(residue) > 0 {
		rec.Residue = residue
	}
	return rec
}

func (h *handler) planReversal(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, h.cfg.Capabilities.Planning) {
		return
	}
	plan, err := h.cfg.Planner.Plan(r.Context(), h.cfg.Principal, h.cfg.Scope, r.PathValue("actionId"))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, plan)
}

func (h *handler) reverse(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, h.cfg.Capabilities.Reversal) {
		return
	}
	// Idempotency-Key (Master Prompt §36): optional but durable when present.
	// The key is scoped per authorization scope; raw keys are never stored.
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idemKey) > 256 {
		h.writeProblem(w, roperr.New(rop.ProblemMalformedPayload, "Idempotency-Key exceeds 256 characters"),
			http.StatusBadRequest)
		return
	}
	result, err := h.cfg.Reversal.Reverse(r.Context(), h.cfg.Principal, h.cfg.Scope, r.PathValue("actionId"), idemKey)
	if err != nil {
		h.fail(w, err)
		return
	}
	// 200 with the semantic outcome in the body: transport success is not
	// semantic success (Master Prompt §28). OUTCOME_UNKNOWN and
	// REVERSE_FAILED are possible bodies of a 200.
	h.writeJSON(w, http.StatusOK, result)
}

func (h *handler) verification(w http.ResponseWriter, r *http.Request) {
	result, err := h.cfg.Verification.Verify(r.Context(), h.cfg.Principal, h.cfg.Scope, r.PathValue("actionId"))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, result)
}

// SweepExpiry exposes server-time expiration sweeping to the host
// application (callable on business mutations, so status is current even
// before a client inspects it).
func SweepExpiry(cfg Config) (int, error) {
	return cfg.Store.SweepExpiry(context.Background(), cfg.Store.DB(), cfg.Clock.Now())
}
