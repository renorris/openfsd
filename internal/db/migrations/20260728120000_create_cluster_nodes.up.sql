CREATE TABLE IF NOT EXISTS cluster_nodes (
  node_id     TEXT PRIMARY KEY,
  fsd_host    TEXT NOT NULL,
  fsd_port    INTEGER NOT NULL DEFAULT 6809,
  service_url TEXT NOT NULL,
  location    TEXT NOT NULL DEFAULT '',
  region      TEXT NOT NULL DEFAULT '',
  is_sweatbox INTEGER NOT NULL DEFAULT 0,
  updated_at  TEXT NOT NULL
);
