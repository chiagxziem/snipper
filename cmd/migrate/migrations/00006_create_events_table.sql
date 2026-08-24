-- +goose Up
CREATE TABLE events (
  id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organizer_id              UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name                      TEXT NOT NULL,
  description               TEXT,
  location                  TEXT NOT NULL,
  status                    TEXT NOT NULL DEFAULT 'draft',
                            -- 'draft', 'published', 'sold_out', 'cancelled', 'ended'
  starts_at                 TIMESTAMPTZ NOT NULL,
  ends_at                   TIMESTAMPTZ NOT NULL,
  capacity                  INTEGER,  -- nullable, NULL means no top-level cap
  cancellation_allowed      BOOLEAN NOT NULL DEFAULT TRUE,
  cancellation_hours_before INTEGER NOT NULL DEFAULT 0,
  max_tickets_per_purchase  INTEGER NOT NULL DEFAULT 10,
  material_changed_at       TIMESTAMPTZ,  -- NULL = no material change since publish
  created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT  valid_dates CHECK (ends_at > starts_at),
  CONSTRAINT  positive_capacity CHECK (capacity IS NULL OR capacity > 0),
  CONSTRAINT  positive_max_tickets CHECK (max_tickets_per_purchase > 0),
  CONSTRAINT  valid_cancellation_hours CHECK (cancellation_hours_before >= 0)
);

CREATE INDEX IF NOT EXISTS idx_events_organizer_id ON events(organizer_id);
CREATE INDEX IF NOT EXISTS idx_events_status ON events(status);
CREATE INDEX IF NOT EXISTS idx_events_starts_at ON events(starts_at);

DROP TRIGGER IF EXISTS update_events_updated_at ON events;
CREATE TRIGGER update_events_updated_at
  BEFORE UPDATE ON events
  FOR EACH ROW
  EXECUTE FUNCTION update_updated_at_column();

-- +goose Down
DROP TABLE IF EXISTS events CASCADE;
DROP TRIGGER IF EXISTS update_events_updated_at ON events;
