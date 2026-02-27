CREATE TABLE IF NOT EXISTS daz_user_economy (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    balance BIGINT NOT NULL DEFAULT 0,
    trust_score INTEGER NOT NULL DEFAULT 50 CHECK (trust_score >= 0 AND trust_score <= 100),
    heist_count INTEGER NOT NULL DEFAULT 0,
    last_heist_at TIMESTAMP,
    total_earned BIGINT NOT NULL DEFAULT 0,
    total_lost BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_user_economy_balance
    ON daz_user_economy(channel, balance DESC);
CREATE INDEX IF NOT EXISTS idx_user_economy_trust
    ON daz_user_economy(channel, trust_score DESC);

CREATE TABLE IF NOT EXISTS daz_user_bladder (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    current_amount INTEGER NOT NULL DEFAULT 0,
    last_drink_time TIMESTAMP,
    drinks_since_piss JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_piss_time TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_user_bladder_amount
    ON daz_user_bladder(channel, current_amount DESC);

CREATE TABLE IF NOT EXISTS daz_pissing_contest_stats (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_matches INTEGER NOT NULL DEFAULT 0,
    wins INTEGER NOT NULL DEFAULT 0,
    losses INTEGER NOT NULL DEFAULT 0,
    money_won BIGINT NOT NULL DEFAULT 0,
    money_lost BIGINT NOT NULL DEFAULT 0,
    best_distance DOUBLE PRECISION NOT NULL DEFAULT 0,
    best_volume DOUBLE PRECISION NOT NULL DEFAULT 0,
    best_aim DOUBLE PRECISION NOT NULL DEFAULT 0,
    best_duration DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_distance DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_volume DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_aim DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_duration DOUBLE PRECISION NOT NULL DEFAULT 0,
    rarest_characteristic VARCHAR(255),
    favorite_location VARCHAR(255),
    biggest_upset_win TEXT,
    legendary_performances JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_played_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_pissing_stats_wins
    ON daz_pissing_contest_stats(channel, wins DESC);
CREATE INDEX IF NOT EXISTS idx_pissing_stats_money_won
    ON daz_pissing_contest_stats(channel, money_won DESC);

CREATE TABLE IF NOT EXISTS daz_mystery_box_stats (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_opens INTEGER NOT NULL DEFAULT 0,
    total_winnings BIGINT NOT NULL DEFAULT 0,
    jackpots_won INTEGER NOT NULL DEFAULT 0,
    bombs_hit INTEGER NOT NULL DEFAULT 0,
    best_win BIGINT NOT NULL DEFAULT 0,
    worst_loss BIGINT NOT NULL DEFAULT 0,
    current_streak INTEGER NOT NULL DEFAULT 0,
    best_streak INTEGER NOT NULL DEFAULT 0,
    last_played_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_mystery_box_winnings
    ON daz_mystery_box_stats(channel, total_winnings DESC);

CREATE TABLE IF NOT EXISTS daz_couch_stats (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_dives INTEGER NOT NULL DEFAULT 0,
    successful_dives INTEGER NOT NULL DEFAULT 0,
    total_found BIGINT NOT NULL DEFAULT 0,
    best_find BIGINT NOT NULL DEFAULT 0,
    injuries INTEGER NOT NULL DEFAULT 0,
    hospital_trips INTEGER NOT NULL DEFAULT 0,
    last_played_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_couch_total_found
    ON daz_couch_stats(channel, total_found DESC);

CREATE TABLE IF NOT EXISTS daz_coin_flip_stats (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_flips INTEGER NOT NULL DEFAULT 0,
    heads_count INTEGER NOT NULL DEFAULT 0,
    tails_count INTEGER NOT NULL DEFAULT 0,
    wins INTEGER NOT NULL DEFAULT 0,
    losses INTEGER NOT NULL DEFAULT 0,
    total_wagered BIGINT NOT NULL DEFAULT 0,
    total_won BIGINT NOT NULL DEFAULT 0,
    total_lost BIGINT NOT NULL DEFAULT 0,
    biggest_win BIGINT NOT NULL DEFAULT 0,
    biggest_loss BIGINT NOT NULL DEFAULT 0,
    current_streak INTEGER NOT NULL DEFAULT 0,
    best_win_streak INTEGER NOT NULL DEFAULT 0,
    worst_loss_streak INTEGER NOT NULL DEFAULT 0,
    pvp_wins INTEGER NOT NULL DEFAULT 0,
    pvp_losses INTEGER NOT NULL DEFAULT 0,
    last_played_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_coin_flip_total_wagered
    ON daz_coin_flip_stats(channel, total_wagered DESC);
CREATE INDEX IF NOT EXISTS idx_coin_flip_wins
    ON daz_coin_flip_stats(channel, wins DESC);

CREATE TABLE IF NOT EXISTS daz_sign_spinning_stats (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_spins INTEGER NOT NULL DEFAULT 0,
    total_earnings BIGINT NOT NULL DEFAULT 0,
    cars_hit INTEGER NOT NULL DEFAULT 0,
    cops_called INTEGER NOT NULL DEFAULT 0,
    best_shift BIGINT NOT NULL DEFAULT 0,
    worst_shift BIGINT NOT NULL DEFAULT 0,
    perfect_days INTEGER NOT NULL DEFAULT 0,
    times_worked INTEGER NOT NULL DEFAULT 0,
    injuries INTEGER NOT NULL DEFAULT 0,
    best_day BIGINT NOT NULL DEFAULT 0,
    last_played_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_sign_spinning_earnings
    ON daz_sign_spinning_stats(channel, total_earnings DESC);

CREATE TABLE IF NOT EXISTS daz_mug_stats (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_mugs INTEGER NOT NULL DEFAULT 0,
    successful_mugs INTEGER NOT NULL DEFAULT 0,
    failed_mugs INTEGER NOT NULL DEFAULT 0,
    times_mugged INTEGER NOT NULL DEFAULT 0,
    defended_mugs INTEGER NOT NULL DEFAULT 0,
    total_stolen BIGINT NOT NULL DEFAULT 0,
    total_lost BIGINT NOT NULL DEFAULT 0,
    biggest_score BIGINT NOT NULL DEFAULT 0,
    times_arrested INTEGER NOT NULL DEFAULT 0,
    hospital_trips INTEGER NOT NULL DEFAULT 0,
    current_heat_level INTEGER NOT NULL DEFAULT 0,
    last_mugged_by VARCHAR(255),
    last_mugged_at TIMESTAMP,
    cops_called INTEGER NOT NULL DEFAULT 0,
    trust_gained INTEGER NOT NULL DEFAULT 0,
    trust_lost INTEGER NOT NULL DEFAULT 0,
    last_played_at TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_mug_total_stolen
    ON daz_mug_stats(channel, total_stolen DESC);
CREATE INDEX IF NOT EXISTS idx_mug_heat_level
    ON daz_mug_stats(channel, current_heat_level DESC);

CREATE TABLE IF NOT EXISTS daz_coin_flip_challenges (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    challenger VARCHAR(255) NOT NULL,
    challenged VARCHAR(255) NOT NULL,
    amount BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    challenger_choice VARCHAR(16),
    challenged_choice VARCHAR(16),
    result VARCHAR(16),
    winner VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMP,
    expires_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_coin_flip_challenges_status
    ON daz_coin_flip_challenges(channel, status);
CREATE INDEX IF NOT EXISTS idx_coin_flip_challenges_users
    ON daz_coin_flip_challenges(channel, challenger, challenged, status);

CREATE TABLE IF NOT EXISTS daz_pissing_contest_challenges (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    challenger VARCHAR(255) NOT NULL,
    challenged VARCHAR(255) NOT NULL,
    amount BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    challenger_characteristic VARCHAR(255),
    challenged_characteristic VARCHAR(255),
    location VARCHAR(255),
    weather VARCHAR(255),
    challenger_distance DOUBLE PRECISION,
    challenger_volume DOUBLE PRECISION,
    challenger_aim DOUBLE PRECISION,
    challenger_duration DOUBLE PRECISION,
    challenger_total INTEGER,
    challenged_distance DOUBLE PRECISION,
    challenged_volume DOUBLE PRECISION,
    challenged_aim DOUBLE PRECISION,
    challenged_duration DOUBLE PRECISION,
    challenged_total INTEGER,
    winner VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pissing_challenges_status
    ON daz_pissing_contest_challenges(channel, status);
CREATE INDEX IF NOT EXISTS idx_pissing_challenges_users
    ON daz_pissing_contest_challenges(channel, challenger, challenged, status);

CREATE TABLE IF NOT EXISTS daz_bong_sessions (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    session_start TIMESTAMP NOT NULL,
    session_end TIMESTAMP NOT NULL,
    cone_count INTEGER NOT NULL DEFAULT 0,
    max_cones_per_hour DOUBLE PRECISION,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username, session_start)
);

CREATE INDEX IF NOT EXISTS idx_bong_sessions_lookup
    ON daz_bong_sessions(channel, username, session_start DESC);

CREATE TABLE IF NOT EXISTS daz_user_bong_streaks (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    current_streak INTEGER NOT NULL DEFAULT 0,
    longest_streak INTEGER NOT NULL DEFAULT 0,
    last_bong_date DATE,
    streak_start_date DATE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(channel, username)
);

CREATE INDEX IF NOT EXISTS idx_user_bong_streaks_current
    ON daz_user_bong_streaks(channel, current_streak DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON daz_user_economy TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_user_bladder TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_pissing_contest_stats TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_mystery_box_stats TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_couch_stats TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_coin_flip_stats TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_sign_spinning_stats TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_mug_stats TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_coin_flip_challenges TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_pissing_contest_challenges TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_bong_sessions TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_user_bong_streaks TO daz_user;

GRANT USAGE ON SEQUENCE daz_user_economy_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_user_bladder_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_pissing_contest_stats_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_mystery_box_stats_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_couch_stats_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_coin_flip_stats_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_sign_spinning_stats_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_mug_stats_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_coin_flip_challenges_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_pissing_contest_challenges_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_bong_sessions_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_user_bong_streaks_id_seq TO daz_user;
