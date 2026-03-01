-- Oddjob stats for economy command
CREATE TABLE IF NOT EXISTS daz_oddjob_stats (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_jobs INTEGER NOT NULL DEFAULT 0,
    total_earnings BIGINT NOT NULL DEFAULT 0,
    failures INTEGER NOT NULL DEFAULT 0,
    last_played_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_oddjob_earnings
    ON daz_oddjob_stats(channel, total_earnings DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON daz_oddjob_stats TO daz_user;
GRANT USAGE ON SEQUENCE daz_oddjob_stats_id_seq TO daz_user;
