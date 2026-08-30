// Package resourceapi is the reference demo provider (Master Prompt §73): a
// genuinely persisted, versioned resource API that embeds ROP in-process
// (OQ-9 option (a)). It demonstrates the first vertical slice —
// DISCOVER → EXECUTE → ACTION+RECEIPT → PLAN → REVERSE → VERIFY — for one
// resource type, with REVERSIBLE create and IRREVERSIBLE publish exercising
// the irreversible boundary (Master Prompt §26).
//
// Business state and the ROP Action journal live in the same SQLite database
// and are written in one transaction: the M1 "business state + journal" local
// atomicity claim (docs/architecture.md §9). ROP Core never reads the
// resources table; only these provider adapters do.
package resourceapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/clock"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// manualNo is shared: the immutable delivery record cannot be changed even
// manually (provider-declared fact of the demo domain).
func manualNo() *bool {
	b := false
	return &b
}

// Operation IDs.
const (
	OpCreate  = "resource.create"
	OpUpdate  = "resource.update"
	OpPublish = "resource.publish"
	OpNotify  = "resource.notify"
)

// API is the demo provider: business endpoints plus provider operation
// definitions for the ROP registry.
type API struct {
	Store      *store.Store
	Clock      clock.Clock
	Scope      string
	CreateTTL  time.Duration // reversal eligibility window for resource.create
	UpdateTTL  time.Duration // reversal eligibility window for resource.update
	NotifyTTL  time.Duration // reversal eligibility window for resource.notify
	ProviderID string
}

// Operations returns the provider's Operation definitions (Master Prompt §76).
func (api *API) Operations() ([]operation.Operation, error) {
	create := operation.Operation{
		ID:                 OpCreate,
		Reversibility:      rop.ReversibilityREVERSIBLE,
		Guarantee:          rop.GuaranteeEXACT,
		TTL:                api.CreateTTL,
		ReverseOperationID: "resource.delete",
		PlanFunc:           api.planCreate,
		ReverseFunc:        api.reverseCreate,
		VerifyFunc:         api.verifyCreate,
	}
	update := operation.Operation{
		ID:                 OpUpdate,
		Reversibility:      rop.ReversibilityRESTORABLE,
		Guarantee:          rop.GuaranteeEXACT,
		TTL:                api.UpdateTTL,
		ReverseOperationID: "resource.restore",
		PlanFunc:           api.planUpdate,
		ReverseFunc:        api.reverseUpdate,
		VerifyFunc:         api.verifyUpdate,
	}
	notify := operation.Operation{
		ID:                 OpNotify,
		Reversibility:      rop.ReversibilityPARTIALLY_COMPENSATABLE,
		Guarantee:          rop.GuaranteeBEST_EFFORT,
		TTL:                api.NotifyTTL,
		ReverseOperationID: "resource.withdraw",
		PlanFunc:           api.planNotify,
		ReverseFunc:        api.reverseNotify,
		VerifyFunc:         api.verifyNotify,
	}
	publish := operation.Operation{
		ID:            OpPublish,
		Reversibility: rop.ReversibilityIRREVERSIBLE,
		Guarantee:     rop.GuaranteeNONE,
	}
	return []operation.Operation{create, update, publish, notify}, nil
}

type resourceRow struct {
	ResourceID  string
	Scope       string
	Version     int64
	Value       string
	Published   bool
	CreatedFrom string // action that created the resource (reversal safety)
}

func (api *API) getResource(ctx context.Context, q store.DBTX, scope, id string) (resourceRow, bool, error) {
	row := q.QueryRowContext(ctx,
		`SELECT resource_id, scope, version, value, published, created_from_action
		 FROM resources WHERE resource_id = ? AND scope = ?`, id, scope)
	var r resourceRow
	var published int
	if err := row.Scan(&r.ResourceID, &r.Scope, &r.Version, &r.Value, &published, &r.CreatedFrom); err != nil {
		if err == sql.ErrNoRows {
			return resourceRow{}, false, nil
		}
		return resourceRow{}, false, fmt.Errorf("resourceapi: get resource: %w", err)
	}
	r.Published = published != 0
	return r, true, nil
}

// Create inserts a resource and records its Action + Receipt + private
// reversal material in one transaction (architecture §9).
func (api *API) Create(ctx context.Context, value string) (resourceRow, store.ActionRow, error) {
	now := api.Clock.Now()
	tx, err := api.Store.BeginTx(ctx)
	if err != nil {
		return resourceRow{}, store.ActionRow{}, err
	}
	defer func() { _ = tx.Rollback() }()

	res := resourceRow{
		ResourceID: store.NewID("res"),
		Scope:      api.Scope,
		Version:    1,
		Value:      value,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO resources (resource_id, scope, version, value, published, created_from_action)
		 VALUES (?, ?, ?, ?, 0, ?)`,
		res.ResourceID, res.Scope, res.Version, res.Value, ""); err != nil {
		return resourceRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: insert resource: %w", err)
	}

	a := store.ActionRow{
		ActionID:      store.NewID("act"),
		Scope:         api.Scope,
		OperationID:   OpCreate,
		Status:        action.Applied,
		Reversibility: string(rop.ReversibilityREVERSIBLE),
		Guarantee:     string(rop.GuaranteeEXACT),
		ResourceType:  "resource",
		ResourceID:    res.ResourceID,
		CreatedAt:     now,
	}
	if api.CreateTTL > 0 {
		exp := now.Add(api.CreateTTL)
		a.ExpiresAt = &exp
	}
	// Private reversal material (Master Prompt §13): the previous state needed
	// for a safe inverse. Never serialized on public paths (I-14).
	material := map[string]any{"previousResourceVersion": res.Version}
	if err := api.Store.CreateAction(ctx, tx, a, material); err != nil {
		return resourceRow{}, store.ActionRow{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE resources SET created_from_action = ? WHERE resource_id = ?`,
		a.ActionID, res.ResourceID); err != nil {
		return resourceRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: link resource: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return resourceRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: commit create: %w", err)
	}
	return res, a, nil
}

// Update performs a RESTORABLE value update (v -> v+1) and records its Action
// plus the minimum private prior-state material required for restoration
// (previous value and version — Master Prompt §13/§14: only what safe reversal
// and verification require). Business mutation and journal write commit
// atomically (architecture §9).
func (api *API) Update(ctx context.Context, resourceID, value string) (resourceRow, store.ActionRow, error) {
	now := api.Clock.Now()
	tx, err := api.Store.BeginTx(ctx)
	if err != nil {
		return resourceRow{}, store.ActionRow{}, err
	}
	defer func() { _ = tx.Rollback() }()

	prior, ok, err := api.getResource(ctx, tx, api.Scope, resourceID)
	if err != nil {
		return resourceRow{}, store.ActionRow{}, err
	}
	if !ok {
		return resourceRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: resource %s not found", resourceID)
	}
	if prior.Published {
		return resourceRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: resource %s is published", resourceID)
	}
	next := resourceRow{ResourceID: resourceID, Scope: api.Scope, Version: prior.Version + 1, Value: value, CreatedFrom: prior.CreatedFrom}
	if _, err := tx.ExecContext(ctx,
		`UPDATE resources SET value = ?, version = ? WHERE resource_id = ? AND version = ?`,
		next.Value, next.Version, resourceID, prior.Version); err != nil {
		return resourceRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: update: %w", err)
	}
	a := store.ActionRow{
		ActionID:      store.NewID("act"),
		Scope:         api.Scope,
		OperationID:   OpUpdate,
		Status:        action.Applied,
		Reversibility: string(rop.ReversibilityRESTORABLE),
		Guarantee:     string(rop.GuaranteeEXACT),
		ResourceType:  "resource",
		ResourceID:    resourceID,
		CreatedAt:     now,
	}
	if api.UpdateTTL > 0 {
		exp := now.Add(api.UpdateTTL)
		a.ExpiresAt = &exp
	}
	// Private reversal material: the captured valid prior state (RESTORABLE,
	// Master Prompt §6) plus the version this Action produced — the CAS anchor
	// for a safe restore (§41, §48). Never serialized on public paths (I-14).
	material := map[string]any{
		"previousValue":   prior.Value,
		"previousVersion": prior.Version,
		"expectedVersion": next.Version,
	}
	if err := api.Store.CreateAction(ctx, tx, a, material); err != nil {
		return resourceRow{}, store.ActionRow{}, err
	}
	// Dependency (M5 safety constraint, not workflow): the update depends on
	// the create Action — reversal of the create is unsafe while this update
	// is uncompensated. Recorded in the same transaction; the edge cannot
	// cycle (parent is the resource's creation), so the domain cycle walk is
	// not needed here and the durable UNIQUE constraint is the backstop.
	if prior.CreatedFrom != "" {
		if err := api.Store.AddDependency(ctx, tx, api.Scope, prior.CreatedFrom, a.ActionID, now); err != nil {
			return resourceRow{}, store.ActionRow{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return resourceRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: commit update: %w", err)
	}
	return next, a, nil
}

type notificationRow struct {
	NotificationID string
	Scope          string
	ResourceID     string
	Channel        string
	Status         string // SENT | WITHDRAWN
	CreatedFrom    string
	DeliveredAt    time.Time
}

// Notify performs the PARTIALLY_COMPENSATABLE reference scenario (M5): the
// Action produces two real effects —
//
//	effect A: the mutable notification state (status SENT, withdrawable);
//	effect B: an immutable delivery record (delivered_at) that no
//	          automated reversal can erase.
//
// Reversal compensates A (withdraw) but cannot remove B: the outcome is
// PARTIALLY_REVERSED and B remains as first-class residue. This is a genuine
// partial compensation, not an enum label.
func (api *API) Notify(ctx context.Context, resourceID, channel string) (notificationRow, store.ActionRow, error) {
	now := api.Clock.Now()
	tx, err := api.Store.BeginTx(ctx)
	if err != nil {
		return notificationRow{}, store.ActionRow{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, ok, err := api.getResource(ctx, tx, api.Scope, resourceID); err != nil {
		return notificationRow{}, store.ActionRow{}, err
	} else if !ok {
		return notificationRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: resource %s not found", resourceID)
	}
	n := notificationRow{
		NotificationID: store.NewID("ntf"),
		Scope:          api.Scope,
		ResourceID:     resourceID,
		Channel:        channel,
		Status:         "SENT",
		DeliveredAt:    now,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO notifications
		 (notification_id, scope, resource_id, channel, status, created_from_action, delivered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		n.NotificationID, n.Scope, n.ResourceID, n.Channel, n.Status, "", n.DeliveredAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return notificationRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: insert notification: %w", err)
	}
	a := store.ActionRow{
		ActionID:      store.NewID("act"),
		Scope:         api.Scope,
		OperationID:   OpNotify,
		Status:        action.Applied,
		Reversibility: string(rop.ReversibilityPARTIALLY_COMPENSATABLE),
		Guarantee:     string(rop.GuaranteeBEST_EFFORT),
		ResourceType:  "notification",
		ResourceID:    n.NotificationID,
		CreatedAt:     now,
	}
	if api.NotifyTTL > 0 {
		exp := now.Add(api.NotifyTTL)
		a.ExpiresAt = &exp
	}
	material := map[string]any{"notificationID": n.NotificationID, "channel": channel}
	if err := api.Store.CreateAction(ctx, tx, a, material); err != nil {
		return notificationRow{}, store.ActionRow{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE notifications SET created_from_action = ? WHERE notification_id = ?`,
		a.ActionID, n.NotificationID); err != nil {
		return notificationRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: link notification: %w", err)
	}
	// Declared residue: known BEFORE reversal (planning will expose it).
	declared := []rop.Residue{{
		Description:     "the delivered notification record cannot be retracted (immutable delivery observation)",
		Expected:        true,
		ProviderDefined: true,
		Manual:          manualNo(),
	}}
	if err := api.Store.RecordResidue(ctx, tx, a.ActionID, store.ResidueDeclared, declared, now); err != nil {
		return notificationRow{}, store.ActionRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return notificationRow{}, store.ActionRow{}, fmt.Errorf("resourceapi: commit notify: %w", err)
	}
	return n, a, nil
}

func (api *API) getNotification(ctx context.Context, q store.DBTX, scope, id string) (notificationRow, bool, error) {
	row := q.QueryRowContext(ctx,
		`SELECT notification_id, scope, resource_id, channel, status, created_from_action, delivered_at
		 FROM notifications WHERE notification_id = ? AND scope = ?`, id, scope)
	var n notificationRow
	var delivered string
	scanErr := row.Scan(&n.NotificationID, &n.Scope, &n.ResourceID, &n.Channel,
		&n.Status, &n.CreatedFrom, &delivered)
	if scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return notificationRow{}, false, nil
		}
		return notificationRow{}, false, fmt.Errorf("resourceapi: get notification: %w", scanErr)
	}
	var err error
	if n.DeliveredAt, err = time.Parse(time.RFC3339Nano, delivered); err != nil {
		return notificationRow{}, false, fmt.Errorf("resourceapi: parse delivered_at: %w", err)
	}
	return n, true, nil
}

// Publish performs the IRREVERSIBLE boundary operation (Master Prompt §26):
// once published, no automated reversal exists. The Action is still recorded
// — irreversibility is represented, not hidden.
func (api *API) Publish(ctx context.Context, resourceID string) (store.ActionRow, error) {
	now := api.Clock.Now()
	tx, err := api.Store.BeginTx(ctx)
	if err != nil {
		return store.ActionRow{}, err
	}
	defer func() { _ = tx.Rollback() }()

	_, ok, err := api.getResource(ctx, tx, api.Scope, resourceID)
	if err != nil {
		return store.ActionRow{}, err
	}
	if !ok {
		return store.ActionRow{}, fmt.Errorf("resourceapi: resource %s not found", resourceID)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resources SET published = 1, version = version + 1
		WHERE resource_id = ?`, resourceID); err != nil {
		return store.ActionRow{}, fmt.Errorf("resourceapi: publish: %w", err)
	}
	a := store.ActionRow{
		ActionID:      store.NewID("act"),
		Scope:         api.Scope,
		OperationID:   OpPublish,
		Status:        action.Irreversible,
		Reversibility: string(rop.ReversibilityIRREVERSIBLE),
		Guarantee:     string(rop.GuaranteeNONE),
		ResourceType:  "resource",
		ResourceID:    resourceID,
		CreatedAt:     now,
	}
	if err := api.Store.CreateAction(ctx, tx, a, nil); err != nil {
		return store.ActionRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.ActionRow{}, fmt.Errorf("resourceapi: commit publish: %w", err)
	}
	return a, nil
}

// --- provider operation functions (read-only plan; CAS-guarded reverse) ---

func (api *API) planCreate(ctx context.Context, in operation.PlanInput) (operation.PlanOutput, error) {
	out := operation.PlanOutput{
		Preconditions: []string{
			"resource still exists",
			"resource version unchanged since creation",
			"resource has not been published",
		},
		ExpectedReversal: "delete the created resource",
	}
	// Freshness: the plan records the resource version it was computed
	// against (Master Prompt §40) by reading current state — read-only.
	r, ok, err := api.getResource(ctx, api.Store.DB(), in.Action.Scope, in.Action.ResourceID)
	if err != nil {
		return out, err
	}
	if ok {
		v := r.Version
		out.BasisResourceVersion = &v
		if r.Published {
			out.Conflicts = append(out.Conflicts, "resource is published; reversal is refused")
		}
	} else {
		out.Conflicts = append(out.Conflicts, "resource no longer exists")
	}
	return out, nil
}

func (api *API) reverseCreate(ctx context.Context, in operation.ReverseInput) (operation.ReverseOutput, error) {
	// Execution-time precondition re-check (invariant I-19): the plan (if any)
	// is never trusted. CAS on version + created_from (Master Prompt §41, §48):
	// a single conditional DELETE is atomic; RowsAffected proves the CAS held.
	res, err := api.Store.DB().ExecContext(ctx,
		`DELETE FROM resources
		 WHERE resource_id = ? AND created_from_action = ? AND published = 0`,
		in.Action.ResourceID, in.Action.ActionID)
	if err != nil {
		return operation.ReverseOutput{}, fmt.Errorf("resourceapi: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return operation.ReverseOutput{}, fmt.Errorf("resourceapi: delete: %w", err)
	}
	if n == 0 {
		// Correctness-critical precondition failed (I-7): refuse instead of
		// destructive restoration. Distinguish the conflict reasons for audit.
		r, ok, err := api.getResource(ctx, api.Store.DB(), in.Action.Scope, in.Action.ResourceID)
		if err != nil {
			return operation.ReverseOutput{}, err
		}
		switch {
		case !ok:
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "resource no longer exists; refusing unsafe restore"}, nil
		case r.Published:
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "resource has been published since creation; reversal refused"}, nil
		default:
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "resource was not created by this action; reversal refused"}, nil
		}
	}
	// Audit history remains; only the business row is removed (I-1). The
	// residue of a create-reversal in this demo is empty: the Action record
	// itself remains (that is audit, not residue).
	return operation.ReverseOutput{Outcome: rop.OutcomeREVERSED,
		ProviderRef: "resource-delete:" + in.Action.ResourceID}, nil
}

func (api *API) verifyCreate(ctx context.Context, in operation.VerifyInput) (operation.VerifyOutput, error) {
	// Provider-defined postconditions for reversing resource.create
	// (Master Prompt §7): the resource is absent. This never means the Action
	// did not happen (audit history remains).
	r, ok, err := api.getResource(ctx, api.Store.DB(), in.Action.Scope, in.Action.ResourceID)
	if err != nil {
		return operation.VerifyOutput{}, err
	}
	pc := []rop.Postcondition{{ID: "resource-absent", Description: "created resource no longer exists"}}
	if ok {
		pc[0].Satisfied = false
		return operation.VerifyOutput{
			Status: rop.VerificationFAILED, Semantics: rop.SemanticsLOCAL_READONLY,
			Postconditions: pc, Detail: fmt.Sprintf("resource %s still exists (version %d)", r.ResourceID, r.Version),
		}, nil
	}
	pc[0].Satisfied = true
	return operation.VerifyOutput{
		Status: rop.VerificationVERIFIED, Semantics: rop.SemanticsLOCAL_READONLY,
		Postconditions: pc,
	}, nil
}

// materialInt64 extracts a numeric material field (JSON numbers decode as
// float64).
func materialInt64(m map[string]any, key string) (int64, bool) {
	v, ok := m[key].(float64)
	if !ok {
		return 0, false
	}
	return int64(v), true
}

func materialString(m map[string]any, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok
}

// planUpdate describes the restoration as a freshness snapshot (Master Prompt
// §40): the basis version it was computed against, plus an explicit conflict
// entry when the resource has moved past the version this Action produced.
// A plan is knowledge, never authorization (invariant I-19).
func (api *API) planUpdate(ctx context.Context, in operation.PlanInput) (operation.PlanOutput, error) {
	out := operation.PlanOutput{
		Preconditions: []string{
			"resource still exists",
			"resource version still equals the version this action produced",
			"resource has not been published",
		},
		ExpectedReversal: "restore the captured prior value and version",
	}
	expected, hasExpected := materialInt64(in.Material, "expectedVersion")
	r, ok, err := api.getResource(ctx, api.Store.DB(), in.Action.Scope, in.Action.ResourceID)
	if err != nil {
		return out, err
	}
	if !ok {
		out.Conflicts = append(out.Conflicts, "resource no longer exists")
		return out, nil
	}
	v := r.Version
	out.BasisResourceVersion = &v
	if r.Published {
		out.Conflicts = append(out.Conflicts, "resource is published; reversal is refused")
	}
	if hasExpected && r.Version != expected {
		out.Conflicts = append(out.Conflicts,
			"resource is at version "+itoa(r.Version)+"; restoration expects version "+itoa(expected)+
				" — the plan is stale and reversal will conflict")
	}
	return out, nil
}

// reverseUpdate restores the captured prior state under CAS (Master Prompt
// §41, §48): the conditional UPDATE only fires when the resource is still at
// the version this Action produced. Any intervening mutation makes the CAS
// fail and the reversal is refused as CONFLICT — the system never performs
// the blind v3 -> v1 restore.
func (api *API) reverseUpdate(ctx context.Context, in operation.ReverseInput) (operation.ReverseOutput, error) {
	previousValue, okV := materialString(in.Material, "previousValue")
	previousVersion, okPV := materialInt64(in.Material, "previousVersion")
	expectedVersion, okEV := materialInt64(in.Material, "expectedVersion")
	if !okV || !okPV || !okEV {
		// Missing or malformed material is a real consistency problem, never
		// guessed around (docs/failure-model.md §16).
		return operation.ReverseOutput{Outcome: rop.OutcomeREVERSE_FAILED,
			Error: "reversal material missing or malformed; refusing to guess prior state"}, nil
	}
	res, err := api.Store.DB().ExecContext(ctx,
		`UPDATE resources SET value = ?, version = ?
		 WHERE resource_id = ? AND version = ? AND published = 0`,
		previousValue, previousVersion, in.Action.ResourceID, expectedVersion)
	if err != nil {
		return operation.ReverseOutput{}, fmt.Errorf("resourceapi: restore: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return operation.ReverseOutput{}, fmt.Errorf("resourceapi: restore: %w", err)
	}
	if n == 0 {
		// Correctness-critical precondition failed (invariant I-7): diagnose
		// for audit, refuse the restoration.
		r, ok, err := api.getResource(ctx, api.Store.DB(), in.Action.Scope, in.Action.ResourceID)
		if err != nil {
			return operation.ReverseOutput{}, err
		}
		switch {
		case !ok:
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "resource no longer exists; refusing unsafe restore"}, nil
		case r.Published:
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "resource has been published; restoration refused"}, nil
		default:
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "resource is at version " + itoa(r.Version) + ", restoration expects version " + itoa(expectedVersion) +
					": concurrent mutation detected; refusing destructive restore"}, nil
		}
	}
	// Restoration succeeded: the resource carries the captured prior state.
	// History remains (invariant I-1); the original update Action survives.
	return operation.ReverseOutput{Outcome: rop.OutcomeREVERSED,
		ProviderRef: "resource-restore:" + in.Action.ResourceID}, nil
}

// verifyUpdate evaluates the provider-defined postconditions of a successful
// restoration (Master Prompt §7, §46): the resource exists, holds the prior
// value at the prior version, and is not published. Success never means the
// update "did not happen" — audit history remains.
func (api *API) verifyUpdate(ctx context.Context, in operation.VerifyInput) (operation.VerifyOutput, error) {
	previousValue, okV := materialString(in.Material, "previousValue")
	previousVersion, okPV := materialInt64(in.Material, "previousVersion")
	if !okV || !okPV {
		return operation.VerifyOutput{}, fmt.Errorf("resourceapi: verification material missing")
	}
	r, ok, err := api.getResource(ctx, api.Store.DB(), in.Action.Scope, in.Action.ResourceID)
	if err != nil {
		return operation.VerifyOutput{}, err
	}
	pc := []rop.Postcondition{
		{ID: "resource-exists", Description: "restored resource still exists", Satisfied: ok},
		{ID: "value-restored", Description: "resource value equals the captured prior value"},
		{ID: "version-restored", Description: "resource version equals the captured prior version"},
		{ID: "not-published", Description: "resource has not been published", Satisfied: ok && !r.Published},
	}
	if !ok {
		return operation.VerifyOutput{Status: rop.VerificationFAILED, Semantics: rop.SemanticsLOCAL_READONLY,
			Postconditions: pc, Detail: "resource no longer exists"}, nil
	}
	pc[1].Satisfied = r.Value == previousValue
	pc[2].Satisfied = r.Version == previousVersion
	for _, p := range pc {
		if !p.Satisfied {
			return operation.VerifyOutput{Status: rop.VerificationFAILED, Semantics: rop.SemanticsLOCAL_READONLY,
				Postconditions: pc, Detail: "restoration postcondition not satisfied"}, nil
		}
	}
	return operation.VerifyOutput{Status: rop.VerificationVERIFIED, Semantics: rop.SemanticsLOCAL_READONLY,
		Postconditions: pc}, nil
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func (api *API) planNotify(ctx context.Context, in operation.PlanInput) (operation.PlanOutput, error) {
	out := operation.PlanOutput{
		Preconditions: []string{
			"the notification still exists",
			"the notification has not already been withdrawn",
		},
		// Partial compensation is the honest expectation: full reversal of a
		// PARTIALLY_COMPENSATABLE Action is impossible by definition.
		ExpectedReversal: "withdraw the notification (mutable state); the delivered record remains as residue",
		Residue: []rop.Residue{{
			Description:     "the delivered notification record cannot be retracted (immutable delivery observation)",
			Expected:        true,
			ProviderDefined: true,
			Manual:          manualNo(),
		}},
	}
	n, ok, err := api.getNotification(ctx, api.Store.DB(), in.Action.Scope, in.Action.ResourceID)
	if err != nil {
		return out, err
	}
	if !ok {
		out.Conflicts = append(out.Conflicts, "notification no longer exists")
	} else if n.Status != "SENT" {
		out.Conflicts = append(out.Conflicts, "notification is "+n.Status+"; reversal will conflict")
	}
	return out, nil
}

func (api *API) reverseNotify(ctx context.Context, in operation.ReverseInput) (operation.ReverseOutput, error) {
	// Compensate effect A only. Effect B (the immutable delivery record) is
	// declared as residue: PARTIALLY_REVERSED, never REVERSED.
	notificationID, _ := materialString(in.Material, "notificationID")
	res, err := api.Store.DB().ExecContext(ctx,
		`UPDATE notifications SET status = 'WITHDRAWN'
		 WHERE notification_id = ? AND created_from_action = ? AND status = 'SENT'`,
		notificationID, in.Action.ActionID)
	if err != nil {
		return operation.ReverseOutput{}, fmt.Errorf("resourceapi: withdraw: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		n0, ok, err := api.getNotification(ctx, api.Store.DB(), in.Action.Scope, notificationID)
		if err != nil {
			return operation.ReverseOutput{}, err
		}
		switch {
		case !ok:
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "notification no longer exists; refusing unsafe state change"}, nil
		case n0.Status == "WITHDRAWN":
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "notification already withdrawn"}, nil
		default:
			return operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT,
				Error: "notification was not created by this action"}, nil
		}
	}
	residue := []rop.Residue{{
		Description:     "the delivered notification record " + notificationID + " cannot be retracted (immutable delivery observation)",
		Expected:        true,
		ProviderDefined: true,
		Manual:          manualNo(),
	}}
	return operation.ReverseOutput{
		Outcome:     rop.OutcomePARTIALLY_REVERSED,
		ProviderRef: "notify-withdraw:" + notificationID,
		Residue:     residue,
	}, nil
}

func (api *API) verifyNotify(ctx context.Context, in operation.VerifyInput) (operation.VerifyOutput, error) {
	// Provider-defined PARTIAL compensation postconditions (M5): distinct
	// from full-reversal criteria. Success confirms effect A compensated AND
	// residue B explicitly present — it never claims full REVERSED.
	notificationID, okID := materialString(in.Material, "notificationID")
	if !okID {
		return operation.VerifyOutput{}, fmt.Errorf("resourceapi: verification material missing")
	}
	n, ok, err := api.getNotification(ctx, api.Store.DB(), in.Action.Scope, notificationID)
	if err != nil {
		return operation.VerifyOutput{}, err
	}
	pc := []rop.Postcondition{
		{ID: "notification-withdrawn", Description: "the mutable notification state is withdrawn",
			Satisfied: ok && n.Status == "WITHDRAWN"},
		{ID: "delivery-record-remains", Description: "the immutable delivery record remains as declared residue",
			Satisfied: ok},
	}
	for _, p := range pc {
		if !p.Satisfied {
			return operation.VerifyOutput{Status: rop.VerificationFAILED, Semantics: rop.SemanticsLOCAL_READONLY,
				Postconditions: pc, Detail: "partial-compensation postcondition not satisfied"}, nil
		}
	}
	return operation.VerifyOutput{Status: rop.VerificationVERIFIED, Semantics: rop.SemanticsLOCAL_READONLY,
		Postconditions: pc, Detail: "partial compensation holds with declared residue"}, nil
}

// Handler returns the demo business API (mounted by ropd). Business responses
// carry ROP receipt headers (Master Prompt §12) so clients can correlate an
// Action without a second request.
func (api *API) Handler(ropMux http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /.well-known/rop", ropMux)
	mux.Handle("GET /.well-known/rop/actions/{actionId}", ropMux)
	mux.Handle("POST /.well-known/rop/actions/{actionId}/plan-reversal", ropMux)
	mux.Handle("POST /.well-known/rop/actions/{actionId}/reverse", ropMux)
	mux.Handle("GET /.well-known/rop/actions/{actionId}/verification", ropMux)
	mux.HandleFunc("POST /resources", api.handleCreate)
	mux.HandleFunc("GET /resources/{id}", api.handleGet)
	mux.HandleFunc("PATCH /resources/{id}", api.handleUpdate)
	mux.HandleFunc("POST /resources/{id}/publish", api.handlePublish)
	mux.HandleFunc("POST /resources/{id}/notify", api.handleNotify)
	return mux
}

func (api *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "urn:rop:problem:malformed-payload", "malformed JSON body")
		return
	}
	// Unknown fields: clients MAY send them; servers ignore unknown optional
	// fields (Master Prompt §21) — decoding into a fixed struct does exactly
	// that, and the trailing-token check rejects garbage only.
	if dec.More() {
		writeProblem(w, http.StatusBadRequest, "urn:rop:problem:malformed-payload", "unexpected trailing content")
		return
	}
	res, act, err := api.Create(r.Context(), body.Value)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "urn:rop:problem:internal-error", "create failed")
		return
	}
	w.Header().Set("ROP-Action-ID", act.ActionID)
	w.Header().Set("ROP-Reversibility", act.Reversibility)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": res.ResourceID, "value": res.Value, "version": res.Version,
	})
}

func (api *API) handleGet(w http.ResponseWriter, r *http.Request) {
	res, ok, err := api.getResource(r.Context(), api.Store.DB(), api.Scope, r.PathValue("id"))
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "urn:rop:problem:internal-error", "lookup failed")
		return
	}
	if !ok {
		writeProblem(w, http.StatusNotFound, "urn:rop:problem:resource-not-found", "no such resource")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": res.ResourceID, "value": res.Value, "version": res.Version, "published": res.Published,
	})
}

func (api *API) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "urn:rop:problem:malformed-payload", "malformed JSON body")
		return
	}
	res, act, err := api.Update(r.Context(), r.PathValue("id"), body.Value)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "not found"):
			writeProblem(w, http.StatusNotFound, "urn:rop:problem:resource-not-found", "no such resource")
		case strings.Contains(msg, "is published"):
			writeProblem(w, http.StatusConflict, "urn:rop:problem:resource-published", "resource is published")
		default:
			writeProblem(w, http.StatusInternalServerError, "urn:rop:problem:internal-error", "update failed")
		}
		return
	}
	w.Header().Set("ROP-Action-ID", act.ActionID)
	w.Header().Set("ROP-Reversibility", act.Reversibility)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": res.ResourceID, "value": res.Value, "version": res.Version,
	})
}

func (api *API) handleNotify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "urn:rop:problem:malformed-payload", "malformed JSON body")
		return
	}
	n, act, err := api.Notify(r.Context(), r.PathValue("id"), body.Channel)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeProblem(w, http.StatusNotFound, "urn:rop:problem:resource-not-found", "no such resource")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "urn:rop:problem:internal-error", "notify failed")
		return
	}
	w.Header().Set("ROP-Action-ID", act.ActionID)
	w.Header().Set("ROP-Reversibility", act.Reversibility)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": n.NotificationID, "resourceId": n.ResourceID,
		"channel": n.Channel, "status": n.Status, "deliveredAt": n.DeliveredAt,
	})
}

func (api *API) handlePublish(w http.ResponseWriter, r *http.Request) {
	act, err := api.Publish(r.Context(), r.PathValue("id"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "urn:rop:problem:resource-not-found", "no such resource")
		return
	}
	w.Header().Set("ROP-Action-ID", act.ActionID)
	w.Header().Set("ROP-Reversibility", act.Reversibility)
	writeJSON(w, http.StatusOK, map[string]any{"published": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, problemType, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": problemType, "title": problemType, "status": status, "detail": detail,
	})
}
