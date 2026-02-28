ALTER TABLE projects
  ADD COLUMN preferred_distributor_id UUID REFERENCES distributors(id);
