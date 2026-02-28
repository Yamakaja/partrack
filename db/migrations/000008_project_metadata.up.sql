ALTER TABLE projects
  ADD COLUMN source_url TEXT,
  ADD COLUMN project_manager_id UUID REFERENCES users(id);
