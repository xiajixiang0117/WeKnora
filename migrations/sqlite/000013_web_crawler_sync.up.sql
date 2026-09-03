-- Mirrors migrations/versioned/000091_web_crawler_sync for Lite mode.
CREATE TABLE IF NOT EXISTS web_crawl_pages (
    id TEXT PRIMARY KEY NOT NULL,
    data_source_id TEXT NOT NULL,
    canonical_url TEXT NOT NULL,
    knowledge_id TEXT,
    title TEXT NOT NULL DEFAULT '',
    last_applied_hash TEXT NOT NULL DEFAULT '',
    last_applied_content TEXT NOT NULL DEFAULT '',
    last_applied_at DATETIME,
    last_seen_scan_id TEXT,
    last_seen_at DATETIME,
    etag TEXT NOT NULL DEFAULT '',
    last_modified TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (data_source_id, canonical_url)
);
CREATE INDEX IF NOT EXISTS idx_web_crawl_pages_data_source_id ON web_crawl_pages (data_source_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_pages_knowledge_id ON web_crawl_pages (knowledge_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_pages_last_seen_scan_id ON web_crawl_pages (last_seen_scan_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_pages_status ON web_crawl_pages (status);

CREATE TABLE IF NOT EXISTS web_crawl_scans (
    id TEXT PRIMARY KEY NOT NULL,
    data_source_id TEXT NOT NULL,
    tenant_id INTEGER NOT NULL,
    initiator_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME,
    items_total INTEGER DEFAULT 0,
    items_added INTEGER DEFAULT 0,
    items_updated INTEGER DEFAULT 0,
    items_missing INTEGER DEFAULT 0,
    items_failed INTEGER DEFAULT 0,
    items_skipped INTEGER DEFAULT 0,
    items_applied INTEGER DEFAULT 0,
    items_ignored INTEGER DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_web_crawl_scans_data_source_id ON web_crawl_scans (data_source_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_scans_tenant_id ON web_crawl_scans (tenant_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_scans_status ON web_crawl_scans (status);

CREATE TABLE IF NOT EXISTS web_crawl_changes (
    id TEXT PRIMARY KEY NOT NULL,
    scan_id TEXT NOT NULL,
    page_id TEXT,
    canonical_url TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    change_type TEXT NOT NULL,
    old_hash TEXT NOT NULL DEFAULT '',
    new_hash TEXT NOT NULL DEFAULT '',
    previous_content TEXT NOT NULL DEFAULT '',
    new_content TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    source_status INTEGER DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    decision TEXT NOT NULL DEFAULT 'pending',
    action TEXT NOT NULL DEFAULT '',
    apply_status TEXT NOT NULL DEFAULT 'pending',
    applied_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_scan_id ON web_crawl_changes (scan_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_page_id ON web_crawl_changes (page_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_type ON web_crawl_changes (change_type);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_decision ON web_crawl_changes (decision);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_apply_status ON web_crawl_changes (apply_status);
