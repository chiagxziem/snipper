-- +goose Up
CREATE TABLE waitlist_entries (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tier_id     UUID NOT NULL REFERENCES ticket_tiers(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'waiting',
              -- 'waiting', 'notified', 'purchased', 'expired'
  notified_at TIMESTAMPTZ,  -- when the user was notified of availability
  expires_at  TIMESTAMPTZ,  -- deadline to complete purchase after notification
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  UNIQUE  (user_id, tier_id) -- one entry per user per tier
);

CREATE INDEX idx_waitlist_entries_tier_id ON waitlist_entries(tier_id);
CREATE INDEX idx_waitlist_entries_status ON waitlist_entries(status);
CREATE INDEX idx_waitlist_entries_created_at ON waitlist_entries(created_at);

DROP TRIGGER IF EXISTS update_waitlist_entries_updated_at ON waitlist_entries;
CREATE TRIGGER update_waitlist_entries_updated_at
  BEFORE UPDATE ON waitlist_entries
  FOR EACH ROW
  EXECUTE FUNCTION update_updated_at_column();

-- +goose Down
DROP TABLE IF EXISTS waitlist_entries CASCADE;
DROP TRIGGER IF EXISTS update_waitlist_entries_updated_at ON waitlist_entries;
