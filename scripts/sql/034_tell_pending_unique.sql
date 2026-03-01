-- Enforce one pending tell per channel/recipient
CREATE UNIQUE INDEX IF NOT EXISTS idx_tell_pending_unique
  ON daz_tell_messages(channel, LOWER(to_user))
  WHERE delivered = FALSE;
