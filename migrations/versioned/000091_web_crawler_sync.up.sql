-- Migration: 000091_web_crawler_sync
-- Reviewable, manual website crawl snapshots and page baselines.
CREATE TABLE IF NOT EXISTS web_crawl_pages (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    data_source_id VARCHAR(36) NOT NULL REFERENCES data_sources(id) ON DELETE CASCADE,
    canonical_url TEXT NOT NULL,
    knowledge_id VARCHAR(36) NULL,
    title VARCHAR(255) NOT NULL DEFAULT '',
    last_applied_hash VARCHAR(64) NOT NULL DEFAULT '',
    last_applied_content TEXT NOT NULL DEFAULT '',
    last_applied_at TIMESTAMP NULL,
    last_seen_scan_id VARCHAR(36) NULL,
    last_seen_at TIMESTAMP NULL,
    etag VARCHAR(512) NOT NULL DEFAULT '',
    last_modified VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_web_crawl_page_url UNIQUE (data_source_id, canonical_url)
);

CREATE INDEX IF NOT EXISTS idx_web_crawl_pages_data_source_id ON web_crawl_pages (data_source_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_pages_knowledge_id ON web_crawl_pages (knowledge_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_pages_last_seen_scan_id ON web_crawl_pages (last_seen_scan_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_pages_status ON web_crawl_pages (status);

CREATE TABLE IF NOT EXISTS web_crawl_scans (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    data_source_id VARCHAR(36) NOT NULL REFERENCES data_sources(id) ON DELETE CASCADE,
    tenant_id BIGINT NOT NULL,
    initiator_id VARCHAR(36) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL,
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP NULL,
    items_total INT DEFAULT 0,
    items_added INT DEFAULT 0,
    items_updated INT DEFAULT 0,
    items_missing INT DEFAULT 0,
    items_failed INT DEFAULT 0,
    items_skipped INT DEFAULT 0,
    items_applied INT DEFAULT 0,
    items_ignored INT DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_web_crawl_scans_data_source_id ON web_crawl_scans (data_source_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_scans_tenant_id ON web_crawl_scans (tenant_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_scans_status ON web_crawl_scans (status);
CREATE INDEX IF NOT EXISTS idx_web_crawl_scans_started_at ON web_crawl_scans (started_at);

CREATE TABLE IF NOT EXISTS web_crawl_changes (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    scan_id VARCHAR(36) NOT NULL REFERENCES web_crawl_scans(id) ON DELETE CASCADE,
    page_id VARCHAR(36) NULL REFERENCES web_crawl_pages(id) ON DELETE SET NULL,
    canonical_url TEXT NOT NULL,
    title VARCHAR(255) NOT NULL DEFAULT '',
    change_type VARCHAR(32) NOT NULL,
    old_hash VARCHAR(64) NOT NULL DEFAULT '',
    new_hash VARCHAR(64) NOT NULL DEFAULT '',
    previous_content TEXT NOT NULL DEFAULT '',
    new_content TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    source_status INT DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    decision VARCHAR(32) NOT NULL DEFAULT 'pending',
    action VARCHAR(32) NOT NULL DEFAULT '',
    apply_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    applied_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_scan_id ON web_crawl_changes (scan_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_page_id ON web_crawl_changes (page_id);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_type ON web_crawl_changes (change_type);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_decision ON web_crawl_changes (decision);
CREATE INDEX IF NOT EXISTS idx_web_crawl_changes_apply_status ON web_crawl_changes (apply_status);
