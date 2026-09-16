package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWebCrawlerChangeTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	// Keep the nullable page reference from the PostgreSQL migration: neither
	// AutoMigrate nor the SQLite migration creates this foreign key.
	for _, statement := range []string{
		`CREATE TABLE web_crawl_scans (id TEXT NOT NULL PRIMARY KEY)`,
		`CREATE TABLE web_crawl_pages (id TEXT NOT NULL PRIMARY KEY)`,
		`CREATE TABLE web_crawl_changes (
			id TEXT NOT NULL PRIMARY KEY,
			scan_id TEXT NOT NULL REFERENCES web_crawl_scans(id) ON DELETE CASCADE,
			page_id TEXT NULL REFERENCES web_crawl_pages(id) ON DELETE SET NULL,
			canonical_url TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			folder_path TEXT NOT NULL DEFAULT '',
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
			applied_at DATETIME NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO web_crawl_scans (id) VALUES ('scan-1')`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	return db
}

func TestWebCrawlerRepositoryCreateChangeWithoutPage(t *testing.T) {
	db := setupWebCrawlerChangeTestDB(t)
	repo := NewWebCrawlerRepository(db)
	ctx := context.Background()
	change := &types.WebCrawlChange{
		ScanID:       "scan-1",
		CanonicalURL: "https://example.com/missing.html",
		ChangeType:   types.WebCrawlChangeFailed,
		SourceStatus: 404,
		ErrorMessage: "HTTP 404 Not Found",
	}

	require.NoError(t, repo.CreateChange(ctx, change))
	require.NotEmpty(t, change.ID)
	var pageID sql.NullString
	require.NoError(t, db.Raw("SELECT page_id FROM web_crawl_changes WHERE id = ?", change.ID).Row().Scan(&pageID))
	require.False(t, pageID.Valid, "a failure without a page must store SQL NULL, not an empty string")

	stored, err := repo.FindChange(ctx, change.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Empty(t, stored.PageID)
	require.Equal(t, change.CanonicalURL, stored.CanonicalURL)
	require.Equal(t, change.ChangeType, stored.ChangeType)
	require.Equal(t, change.SourceStatus, stored.SourceStatus)
	require.Equal(t, change.ErrorMessage, stored.ErrorMessage)

	changes, err := repo.ListChanges(ctx, change.ScanID, "", "", "", 50, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Empty(t, changes[0].PageID)
}

func TestWebCrawlerRepositoryCreateChangePreservesPageReference(t *testing.T) {
	db := setupWebCrawlerChangeTestDB(t)
	repo := NewWebCrawlerRepository(db)
	ctx := context.Background()
	require.NoError(t, db.Exec("INSERT INTO web_crawl_pages (id) VALUES (?)", "page-1").Error)
	change := &types.WebCrawlChange{
		ScanID:       "scan-1",
		PageID:       "page-1",
		CanonicalURL: "https://example.com/page.html",
		ChangeType:   types.WebCrawlChangeUpdated,
	}

	require.NoError(t, repo.CreateChange(ctx, change))
	var pageID sql.NullString
	require.NoError(t, db.Raw("SELECT page_id FROM web_crawl_changes WHERE id = ?", change.ID).Row().Scan(&pageID))
	require.True(t, pageID.Valid)
	require.Equal(t, change.PageID, pageID.String)
	stored, err := repo.FindChange(ctx, change.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, change.PageID, stored.PageID)
}

func TestWebCrawlerRepositoryCreateChangeRejectsUnknownPage(t *testing.T) {
	db := setupWebCrawlerChangeTestDB(t)
	repo := NewWebCrawlerRepository(db)
	change := &types.WebCrawlChange{
		ScanID:       "scan-1",
		PageID:       "unknown-page",
		CanonicalURL: "https://example.com/page.html",
		ChangeType:   types.WebCrawlChangeUpdated,
	}

	err := repo.CreateChange(context.Background(), change)
	require.ErrorContains(t, err, "FOREIGN KEY constraint failed")
	var count int64
	require.NoError(t, db.Model(&types.WebCrawlChange{}).Count(&count).Error)
	require.Zero(t, count)
}
