CREATE TABLE IF NOT EXISTS custom_pod_templates (
  id TEXT PRIMARY KEY,
  manifest_json TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);
