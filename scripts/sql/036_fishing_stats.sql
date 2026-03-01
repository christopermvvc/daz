-- Fishing stats for economy command
CREATE TABLE IF NOT EXISTS daz_fishing_stats (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_casts INTEGER NOT NULL DEFAULT 0,
    total_catches INTEGER NOT NULL DEFAULT 0,
    total_earnings BIGINT NOT NULL DEFAULT 0,
    best_catch BIGINT NOT NULL DEFAULT 0,
    rare_catches INTEGER NOT NULL DEFAULT 0,
    legendary_catches INTEGER NOT NULL DEFAULT 0,
    misses INTEGER NOT NULL DEFAULT 0,
    last_played_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_fishing_earnings
    ON daz_fishing_stats(channel, total_earnings DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON daz_fishing_stats TO daz_user;
GRANT USAGE ON SEQUENCE daz_fishing_stats_id_seq TO daz_user;
