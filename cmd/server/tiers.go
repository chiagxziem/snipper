package main

import (
	"fmt"
	"net/http"

	"github.com/goziemsunday/gater/internal/jsonutil"
	"github.com/goziemsunday/gater/internal/store"
)

type CreateTierPayload struct {
	Name     string `json:"name" validate:"required,max=255"`
	Price    *int   `json:"price" validate:"omitempty,gte=0"`
	Quantity *int   `json:"quantity" validate:"omitempty,gt=0"`
}

func (a *application) createTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := loggerFromCtx(ctx)

	// the event, loaded by requireEventOrganizer
	event, ok := ctx.Value(eventCtx).(*store.Event)
	if !ok {
		logger.Error("failed to get event from context")
		jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	var payload CreateTierPayload
	if err := jsonutil.Read(w, r, &payload); err != nil {
		jsonutil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if errs, ok := a.validator.ValidateStruct(&payload); !ok {
		jsonutil.WriteErrors(w, http.StatusUnprocessableEntity, errs)
		return
	}

	// tiers can only be added while the event is still a draft
	if event.Status != "draft" {
		jsonutil.WriteError(w, http.StatusConflict, "tiers can only be added to draft events")
		return
	}

	if payload.Price == nil {
		jsonutil.WriteError(w, http.StatusUnprocessableEntity, "price is required")
		return
	}
	if payload.Quantity == nil {
		jsonutil.WriteError(w, http.StatusUnprocessableEntity, "quantity is required")
		return
	}

	// if the event has a capacity cap, the total tier inventory
	// (existing tiers + this new one) must not exceed it
	if event.Capacity != nil {
		existingTotal, err := a.store.Tiers.SumQuantityByEvent(ctx, event.ID.String())
		if err != nil {
			logger.Error("failed to sum tier quantities", "error", err, "event_id", event.ID)
			jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
			return
		}

		if existingTotal+*payload.Quantity > *event.Capacity {
			jsonutil.WriteError(w, http.StatusBadRequest,
				fmt.Sprintf("total tier capacity (%d) would exceed event capacity (%d)",
					existingTotal+*payload.Quantity, *event.Capacity))
			return
		}
	}

	tier := &store.Tier{
		EventID:   event.ID,
		Name:      payload.Name,
		Price:     *payload.Price,
		Quantity:  *payload.Quantity,
		Remaining: *payload.Quantity, // a brand-new tier is fully in stock
	}

	if err := a.store.Tiers.Create(ctx, tier); err != nil {
		logger.Error("failed to create tier", "error", err, "event_id", event.ID)
		jsonutil.WriteError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	type returnData struct {
		Message string      `json:"message"`
		Tier    *store.Tier `json:"tier"`
	}
	jsonutil.WriteData(w, http.StatusCreated, returnData{
		Message: "tier created successfully",
		Tier:    tier,
	})
}
