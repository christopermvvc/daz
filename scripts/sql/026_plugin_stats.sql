-- Create plugin statistics table
CREATE TABLE IF NOT EXISTS daz_plugin_stats (
    plugin_name VARCHAR(100) PRIMARY KEY,
    events_processed BIGINT DEFAULT 0,
    events_failed BIGINT DEFAULT 0,
    retries_scheduled BIGINT DEFAULT 0,
    retries_succeeded BIGINT DEFAULT 0,
    retries_failed BIGINT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Initialize stats for known plugins
INSERT INTO daz_plugin_stats (plugin_name)
VALUES 
    ('retry'),
    ('sql'),
    ('eventfilter'),
    ('usertracker'),
    ('mediatracker'),
    ('analytics'),
    ('about'),
    ('help'),
    ('uptime'),
    ('debug')
ON CONFLICT (plugin_name) DO NOTHING;

-- Grant permissions
GRANT SELECT, INSERT, UPDATE ON daz_plugin_stats TO daz_user;