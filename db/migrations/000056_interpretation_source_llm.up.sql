-- Allow 'llm' as a bom_line_items.interpretation_source, for interpretations
-- produced by the LLM fallback (see doc/bom-import.md). Like 'user', an 'llm'
-- interpretation is never clobbered by a re-parse, and is remembered team-wide.
ALTER TABLE bom_line_items DROP CONSTRAINT bom_line_items_interpretation_source_check;
ALTER TABLE bom_line_items ADD CONSTRAINT bom_line_items_interpretation_source_check
    CHECK (interpretation_source IN ('auto', 'user', 'llm'));
