CREATE TABLE IF NOT EXISTS daz_player_state (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    bladder BIGINT NOT NULL DEFAULT 0 CHECK (bladder >= 0),
    alcohol BIGINT NOT NULL DEFAULT 0 CHECK (alcohol >= 0),
    weed BIGINT NOT NULL DEFAULT 0 CHECK (weed >= 0),
    food BIGINT NOT NULL DEFAULT 0 CHECK (food >= 0),
    lust BIGINT NOT NULL DEFAULT 0 CHECK (lust >= 0),
    last_bladder_mutation_at TIMESTAMP,
    last_alcohol_mutation_at TIMESTAMP,
    last_weed_mutation_at TIMESTAMP,
    last_food_mutation_at TIMESTAMP,
    last_lust_mutation_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

ALTER TABLE daz_player_state
    ADD COLUMN IF NOT EXISTS lust BIGINT NOT NULL DEFAULT 0 CHECK (lust >= 0),
    ADD COLUMN IF NOT EXISTS last_lust_mutation_at TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_player_state_channel_lookup
    ON daz_player_state (channel, username);
CREATE INDEX IF NOT EXISTS idx_player_state_updated
    ON daz_player_state (updated_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON daz_player_state TO daz_user;
GRANT USAGE ON SEQUENCE daz_player_state_id_seq TO daz_user;
