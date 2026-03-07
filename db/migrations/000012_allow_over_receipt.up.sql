-- Allow receiving more items than ordered (e.g. distributor ships extras).
ALTER TABLE order_line_items DROP CONSTRAINT IF EXISTS received_not_exceeds_ordered;
