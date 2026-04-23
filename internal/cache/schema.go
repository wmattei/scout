package cache

const schemaVersion = 2 // bumped from index/ schema v1

const schemaDDL = `
CREATE TABLE IF NOT EXISTS rows (
    package_id TEXT NOT NULL,
    key        TEXT NOT NULL,
    name       TEXT NOT NULL,
    meta_json  TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (package_id, key)
);
CREATE INDEX IF NOT EXISTS rows_pkg_name_idx ON rows(package_id, name);
CREATE INDEX IF NOT EXISTS rows_pkg_key_prefix_idx ON rows(package_id, key);

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);
`
