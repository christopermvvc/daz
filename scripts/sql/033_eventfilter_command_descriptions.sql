-- Add description field for eventfilter command registry
ALTER TABLE daz_eventfilter_commands
ADD COLUMN IF NOT EXISTS description TEXT;
