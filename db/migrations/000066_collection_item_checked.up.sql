-- Checkable list items: a persisted, cross-device selection on list rows.
--
-- Ordinary table selection is ephemeral and per-tab — which is precisely what a
-- scanner on a *different* device cannot write into. Persisting the selection is
-- what makes the handoff work: sort a pile on the phone, walk back to the PC,
-- and act on what you found. NULL = unchecked; the timestamp is kept (rather
-- than a bool) for the same reason quantity is nullable — it records when, not
-- just whether.
ALTER TABLE part_collection_items ADD COLUMN checked_at TIMESTAMPTZ;
