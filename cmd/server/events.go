package main

import (
	"net/http"
	"strconv"
	"time"

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
	events, err := a.store.Events.GetPublished(ctx, cur, limit+1)
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
