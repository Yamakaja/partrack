ALTER TABLE scan_sessions ADD COLUMN IF NOT EXISTS context_type TEXT;
ALTER TABLE scan_sessions ADD COLUMN IF NOT EXISTS context_order_id UUID;
ALTER TABLE scan_sessions ADD COLUMN IF NOT EXISTS context_order_ref TEXT;
