-- Create table to track Ollama responses to avoid duplicate responses
CREATE TABLE IF NOT EXISTS daz_ollama_responses (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    message_hash VARCHAR(64) NOT NULL, -- SHA256 hash of the message to detect duplicates
    message_time BIGINT NOT NULL, -- Unix timestamp from the chat message
    responded_at TIMESTAMP NOT NULL DEFAULT NOW(),
    response_text TEXT,
    UNIQUE(channel, username, message_hash, message_time)
);

-- Index for efficient lookups
CREATE INDEX IF NOT EXISTS idx_ollama_responses_lookup 
    ON daz_ollama_responses(channel, username, message_time DESC);

-- Index for cleanup of old responses
CREATE INDEX IF NOT EXISTS idx_ollama_responses_cleanup
    ON daz_ollama_responses(responded_at);

-- Table for rate limiting (per user per minute)
CREATE TABLE IF NOT EXISTS daz_ollama_rate_limits (
    id SERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    last_response_at TIMESTAMP NOT NULL,
    UNIQUE(channel, username)
);