package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/goziemsunday/gater/internal/mailer"
	"github.com/goziemsunday/gater/internal/store"
	"github.com/hibiken/asynq"
)

type NotifyBuyersUpdatedPayload struct {
	EventID           uuid.UUID         `json:"event_id"`
	EventName         string            `json:"event_name"`
	ChangedFields     map[string]string `json:"changed_fields"`
	MaterialChangedAt time.Time         `json:"material_changed_at"`
}

func NewNotifyBuyersUpdatedTask(
	eventID uuid.UUID,
	eventName string,
	changedFields map[string]string,
	materialChangedAt time.Time,
) (*asynq.Task, error) {
	payload, err := json.Marshal(NotifyBuyersUpdatedPayload{
		EventID:           eventID,
		EventName:         eventName,
		ChangedFields:     changedFields,
		MaterialChangedAt: materialChangedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("worker: notify buyers updated: marshal: %w", err)
	}

	return asynq.NewTask(TypeNotifyBuyersUpdated, payload), nil
}

func HandleNotifyBuyersUpdated(s store.Store, m mailer.Mailer, l *slog.Logger) asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		var p NotifyBuyersUpdatedPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return fmt.Errorf("worker: handle notify buyers updated: unmarshal: %w", err)
		}

		// resolve affected buyers at execution time
		buyers, err := s.Purchases.ListConfirmedBuyersByEvent(ctx, p.EventID.String())
		if err != nil {
			return fmt.Errorf("worker: handle notify buyers updated: list buyers: %w", err)
		}

		for _, buyer := range buyers {
			err := m.SendEventUpdatedNotification(
				ctx,
				[]string{buyer.Email},
				buyer.Name,
				p.EventName,
				p.ChangedFields,
				p.MaterialChangedAt,
			)
			if err != nil {
				l.Error(
					"failed to send event updated notification",
					"error", err,
					"event_id", p.EventID,
					"user_id", buyer.ID,
					"email", buyer.Email,
				)
				continue
			}
			l.Info(
				"sent event updated notification",
				"event_id", p.EventID,
				"user_id", buyer.ID,
			)
		}

		return nil
	}
}
