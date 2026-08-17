package main

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/goziemsunday/gater/internal/cursor"
	"github.com/goziemsunday/gater/internal/jsonutil"
	"github.com/goziemsunday/gater/internal/store"
)

type CreateEventPayload struct {
	Name                    string    `json:"name" validate:"required,max=255"`
	Description             *string   `json:"description"`
	Location                string    `json:"location" validate:"required,max=255"`
	StartsAt                time.Time `json:"starts_at" validate:"required"`
	EndsAt                  time.Time `json:"ends_at" validate:"required"`
	Capacity                *int      `json:"capacity" validate:"omitempty,gte=1"`
	CancellationAllowed     *bool     `json:"cancellation_allowed"`
	CancellationHoursBefore int       `json:"cancellation_hours_before" validate:"gte=0"`
	MaxTicketsPerPurchase   *int      `json:"max_tickets_per_purchase" validate:"omitempty,gte=1"`
}

func (a *application) createEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := loggerFromCtx(ctx)

	// the authenticated organizer, set on the context by requireAuth,
	// and validated to be an organizer by requireOrganizer
	user, ok := ctx.Value(userCtx).(*store.User)
	if !ok {
		logger.Error("failed to get user from context")
		jsonutil.WriteError(w, http.StatusInternalServerError, "user not found in context")
		return
	}

	var payload CreateEventPayload
	if err := jsonutil.Read(w, r, &payload); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if errs, ok := a.validator.ValidateStruct(&payload); !ok {
		jsonutil.WriteErrors(w, http.StatusUnprocessableEntity, errs)
		return
	}

	// ensure that the ends_at datetime is after starts_at
	if !payload.EndsAt.After(payload.StartsAt) {
		jsonutil.WriteError(w, http.StatusUnprocessableEntity, "ends_at must be after starts_at")
		return
	}

	// resolve omitted optional fields to their DB column defaults
	// (cancellation_allowed=true, max_tickets_per_purchase=10)
	cancellationAllowed := true
	if payload.CancellationAllowed != nil {
		cancellationAllowed = *payload.CancellationAllowed
	}
	maxTickets := 10
	if payload.MaxTicketsPerPurchase != nil {
		maxTickets = *payload.MaxTicketsPerPurchase
	}

	event := &store.Event{
		OrganizerID:             user.ID,
		Name:                    payload.Name,
		Description:             payload.Description,
		Location:                payload.Location,
		StartsAt:                payload.StartsAt,
		EndsAt:                  payload.EndsAt,
		Capacity:                payload.Capacity,
		CancellationAllowed:     cancellationAllowed,
		CancellationHoursBefore: payload.CancellationHoursBefore,
		MaxTicketsPerPurchase:   maxTickets,
	}

	if err := a.store.Events.Create(ctx, event); err != nil {
		logger.Error("failed to create event", "error", err, "user_id", user.ID)
		jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	type returnData struct {
		Message string       `json:"message"`
		Event   *store.Event `json:"event"`
	}
	jsonutil.WriteData(w, http.StatusCreated, returnData{
		Message: "event created successfully",
		Event:   event,
	})
}

type UpdateEventPayload struct {
	Name                    *string    `json:"name" validate:"omitempty,max=255"`
	Description             *string    `json:"description"`
	Location                *string    `json:"location" validate:"omitempty,max=255"`
	StartsAt                *time.Time `json:"starts_at"`
	EndsAt                  *time.Time `json:"ends_at"`
	Capacity                *int       `json:"capacity" validate:"omitempty,gte=1"`
	CancellationAllowed     *bool      `json:"cancellation_allowed"`
	CancellationHoursBefore *int       `json:"cancellation_hours_before" validate:"omitempty,gte=0"`
	MaxTicketsPerPurchase   *int       `json:"max_tickets_per_purchase" validate:"omitempty,gte=1"`
	ConfirmMaterialChange   *bool      `json:"confirm_material_change"`
}

func (a *application) updateEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := loggerFromCtx(ctx)

	// the event, loaded by requireEventOrganizer
	event, ok := ctx.Value(eventCtx).(*store.Event)
	if !ok {
		logger.Error("failed to get event from context")
		jsonutil.WriteError(w, http.StatusInternalServerError, "event not found in context")
		return
	}

	var payload UpdateEventPayload
	if err := jsonutil.Read(w, r, &payload); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if errs, ok := a.validator.ValidateStruct(&payload); !ok {
		jsonutil.WriteErrors(w, http.StatusUnprocessableEntity, errs)
		return
	}

	// merge the non-nil fields onto the current event; material fields are
	// tracked so the published-event gate can reject them without confirmation
	changedMaterial := false

	if payload.Name != nil {
		event.Name = *payload.Name
	}
	if payload.Description != nil {
		event.Description = payload.Description
	}
	if payload.Location != nil {
		changedMaterial = true
		event.Location = *payload.Location
	}
	if payload.StartsAt != nil {
		changedMaterial = true
		event.StartsAt = *payload.StartsAt
	}
	if payload.EndsAt != nil {
		changedMaterial = true
		event.EndsAt = *payload.EndsAt
	}
	if payload.Capacity != nil {
		// TODO (Phase 9): reject if the new capacity is below tickets already sold
		event.Capacity = payload.Capacity
	}
	if payload.CancellationAllowed != nil {
		changedMaterial = true
		event.CancellationAllowed = *payload.CancellationAllowed
	}
	if payload.CancellationHoursBefore != nil {
		changedMaterial = true
		event.CancellationHoursBefore = *payload.CancellationHoursBefore
	}
	if payload.MaxTicketsPerPurchase != nil {
		event.MaxTicketsPerPurchase = *payload.MaxTicketsPerPurchase
	}

	// ends_at must stay after starts_at once the merge is applied
	if !event.EndsAt.After(event.StartsAt) {
		jsonutil.WriteError(w, http.StatusUnprocessableEntity, "ends_at must be after starts_at")
		return
	}

	switch event.Status {
	case "draft":
		// drafts have no buyers; every field is freely editable
	case "published", "sold_out":
		// material changes require explicit confirmation; without it, reject
		if changedMaterial && (payload.ConfirmMaterialChange == nil || !*payload.ConfirmMaterialChange) {
			// TODO (Phase 9): include the number of confirmed ticket holders affected
			jsonutil.WriteError(w, http.StatusConflict,
				"this change affects confirmed ticket holders; pass confirm_material_change: true to proceed")
			return
		}

		// TODO (Phase 9): open a grace-period cancellation window for affected buyers
		// TODO (Phase 12): enqueue buyer notification with a diff of the changed fields
	default: // cancelled, ended
		jsonutil.WriteError(w, http.StatusConflict, "event can no longer be updated")
		return
	}

	if err := a.store.Events.Update(ctx, event); err != nil {
		logger.Error("failed to update event", "error", err, "event_id", event.ID)
		jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	type returnData struct {
		Message string       `json:"message"`
		Event   *store.Event `json:"event"`
	}
	jsonutil.WriteData(w, http.StatusOK, returnData{
		Message: "event updated successfully",
		Event:   event,
	})
}

func (a *application) getEvent(w http.ResponseWriter, r *http.Request) {
	// runs behind maybeAuth, so a guest gets nil user from the context

	ctx := r.Context()
	logger := loggerFromCtx(ctx)

	// ensure the {id} route param is a real UUID
	idParam := chi.URLParam(r, "id")
	if _, err := uuid.Parse(idParam); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid event id")
		return
	}

	event, err := a.store.Events.GetByID(ctx, idParam)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			jsonutil.WriteError(w, http.StatusNotFound, "event not found")
		default:
			logger.Error("failed to get event", "error", err, "event_id", idParam)
			jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		}
		return
	}

	// unpublished events are hidden from everyone but the organizer;
	// 404 instead of 403 so their existence isn't leaked to guests
	_, user, _ := a.authenticate(r)

	if event.Status != "published" && event.Status != "sold_out" && event.Status != "ended" {
		if user == nil || user.ID != event.OrganizerID {
			jsonutil.WriteError(w, http.StatusNotFound, "event not found")
			return
		}
	}

	type returnData struct {
		Message string       `json:"message"`
		Event   *store.Event `json:"event"`
	}
	jsonutil.WriteData(w, http.StatusOK, returnData{
		Message: "event retrieved successfully",
		Event:   event,
	})
}

func (a *application) getPublishedEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := loggerFromCtx(ctx)

	limit := 50
	if lq := r.URL.Query().Get("limit"); lq != "" {
		if v, err := strconv.Atoi(lq); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	var cur *cursor.Cursor
	if cq := r.URL.Query().Get("cursor"); cq != "" {
		c, err := cursor.Decode(cq)
		if err != nil {
			jsonutil.WriteError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		cur = &c
	}

	// get events up to limit + 1 to ensure there's a next page
	events, err := a.store.Events.GetAllPublished(ctx, cur, limit+1)
	if err != nil {
		logger.Error("failed to get published events", "error", err)
		jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	// if there's a next page, create next cursor
	var nextCursor string
	if len(events) == limit+1 {
		events = events[:limit]
		last := events[len(events)-1]
		c := cursor.Cursor{
			Timestamp: last.StartsAt,
			ID:        last.ID,
		}
		nextCursor = cursor.Encode(c)
	}

	type returnData struct {
		Message    string         `json:"message"`
		Events     []*store.Event `json:"events"`
		NextCursor string         `json:"next_cursor"`
	}
	jsonutil.WriteData(w, http.StatusOK, returnData{
		Message:    "published events retrieved successfully",
		Events:     events,
		NextCursor: nextCursor,
	})
}
