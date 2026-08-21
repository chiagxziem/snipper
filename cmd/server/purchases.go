package main

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/goziemsunday/gater/internal/jsonutil"
	"github.com/goziemsunday/gater/internal/qr"
	"github.com/goziemsunday/gater/internal/store"
)

type CreatePurchasePayload struct {
	TierID   string `json:"tier_id" validate:"required"`
	Quantity int    `json:"quantity" validate:"required,gte=1"`
}

func (a *application) createPurchase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := loggerFromCtx(ctx)

	// the authenticated user, set on the context by requireAuth
	user, ok := ctx.Value(userCtx).(*store.User)
	if !ok {
		logger.Error("failed to get user from context")
		jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	var payload CreatePurchasePayload
	if err := jsonutil.Read(w, r, &payload); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if errs, ok := a.validator.ValidateStruct(&payload); !ok {
		jsonutil.WriteErrors(w, http.StatusUnprocessableEntity, errs)
		return
	}

	// ensure the tierID is a real UUID
	if _, err := uuid.Parse(payload.TierID); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, "invalid tier id")
		return
	}

	tier, err := a.store.Tiers.GetByID(ctx, payload.TierID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			jsonutil.WriteError(w, http.StatusNotFound, "tier not found")
		default:
			logger.Error("failed to get tier", "error", err, "tier_id", payload.TierID)
			jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		}
		return
	}

	event, err := a.store.Events.GetByID(ctx, tier.EventID.String())
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			jsonutil.WriteError(w, http.StatusNotFound, "event not found")
		default:
			logger.Error("failed to get event", "error", err, "event_id", tier.EventID)
			jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		}
		return
	}

	if event.Status != "published" {
		jsonutil.WriteError(w, http.StatusConflict, store.ErrEventNotPublished.Error())
		return
	}

	purchase := &store.Purchase{
		ID:       uuid.New(),
		UserID:   user.ID,
		TierID:   tier.ID,
		Quantity: payload.Quantity,
	}

	var tickets []store.Ticket
	for range payload.Quantity {
		ticketID := uuid.New()
		t := store.Ticket{
			ID:         ticketID,
			PurchaseID: purchase.ID,
			TierID:     tier.ID,
			QRToken:    qr.GenerateToken(ticketID, purchase.ID, tier.ID, a.config.TicketSecret),
		}
		tickets = append(tickets, t)
	}

	if err := a.store.Purchases.Create(ctx, purchase, tickets); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			jsonutil.WriteError(w, http.StatusNotFound, "tier not found")
		case errors.Is(err, store.ErrEventNotPublished):
			jsonutil.WriteError(w, http.StatusConflict, store.ErrEventNotPublished.Error())
		case errors.Is(err, store.ErrExceedsMaxPerPurchase):
			jsonutil.WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("quantity exceeds the maximum tickets allowed per purchase (%d)", event.MaxTicketsPerPurchase))
		case errors.Is(err, store.ErrInsufficientRemaining):
			jsonutil.WriteError(w, http.StatusConflict, store.ErrInsufficientRemaining.Error())
		default:
			logger.Error("failed to create purchase", "error", err, "tier_id", tier.ID, "user_id", user.ID)
			jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		}
		return
	}

	type returnData struct {
		ID        uuid.UUID      `json:"id"`
		Quantity  int            `json:"quantity"`
		Total     int            `json:"total"`
		Status    string         `json:"status"`
		Event     store.Event    `json:"event"`
		Tier      store.Tier     `json:"tier"`
		Tickets   []store.Ticket `json:"tickets"`
		CreatedAt time.Time      `json:"created_at"`
		UpdatedAt time.Time      `json:"updated_at"`
	}
	jsonutil.WriteData(w, http.StatusCreated, returnData{
		ID:        purchase.ID,
		Quantity:  purchase.Quantity,
		Total:     purchase.Total,
		Status:    purchase.Status,
		Event:     *event,
		Tier:      *tier,
		Tickets:   tickets,
		CreatedAt: purchase.CreatedAt,
		UpdatedAt: purchase.UpdatedAt,
	})
}
