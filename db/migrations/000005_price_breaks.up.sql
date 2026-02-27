ALTER TABLE distributor_listings
    ADD COLUMN price_breaks JSONB NOT NULL DEFAULT '[]'::jsonb;
