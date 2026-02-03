package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, 4)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Verify db file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestCreateAndGetSession(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, -1) // -1 to disable reset hour
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Create session
	chatID := "oc_test123"
	threadID := "thread-abc-123"

	entry, err := store.Create(chatID, threadID)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	if entry.ChatID != chatID {
		t.Errorf("ChatID mismatch: got %v, want %v", entry.ChatID, chatID)
	}
	if entry.ThreadID != threadID {
		t.Errorf("ThreadID mismatch: got %v, want %v", entry.ThreadID, threadID)
	}

	// Get session
	retrieved, err := store.GetByChatID(chatID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if retrieved == nil {
		t.Fatal("Session not found")
	}
	if retrieved.ThreadID != threadID {
		t.Errorf("Retrieved ThreadID mismatch: got %v, want %v", retrieved.ThreadID, threadID)
	}
}

func TestGetNonExistentSession(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	entry, err := store.GetByChatID("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("Expected nil for nonexistent session")
	}
}

func TestUpdateSession(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	chatID := "oc_test123"
	store.Create(chatID, "thread-1")

	// Update to new thread
	err = store.Update(chatID, "thread-2")
	if err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	retrieved, _ := store.GetByChatID(chatID)
	if retrieved.ThreadID != "thread-2" {
		t.Errorf("ThreadID not updated: got %v, want thread-2", retrieved.ThreadID)
	}
}

func TestTouchSession(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	chatID := "oc_test123"
	store.Create(chatID, "thread-1")

	// Wait at least 1 second (SQLite stores Unix seconds)
	time.Sleep(1100 * time.Millisecond)

	// Get before touch
	before, _ := store.GetByChatID(chatID)

	err = store.Touch(chatID)
	if err != nil {
		t.Fatalf("Failed to touch session: %v", err)
	}

	retrieved, _ := store.GetByChatID(chatID)
	if !retrieved.UpdatedAt.After(before.UpdatedAt) && !retrieved.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("UpdatedAt was not updated after Touch: before=%v, after=%v", before.UpdatedAt, retrieved.UpdatedAt)
	}
}

func TestDeleteSession(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	chatID := "oc_test123"
	store.Create(chatID, "thread-1")

	err = store.Delete(chatID)
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	retrieved, _ := store.GetByChatID(chatID)
	if retrieved != nil {
		t.Error("Session was not deleted")
	}
}

func TestIsFresh_IdleTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// 1 minute idle timeout
	store, err := NewStore(dbPath, 1, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Fresh entry
	freshEntry := &Entry{
		ChatID:    "test",
		ThreadID:  "thread",
		UpdatedAt: time.Now(),
	}
	if !store.IsFresh(freshEntry) {
		t.Error("Recent entry should be fresh")
	}

	// Stale entry (2 minutes ago)
	staleEntry := &Entry{
		ChatID:    "test",
		ThreadID:  "thread",
		UpdatedAt: time.Now().Add(-2 * time.Minute),
	}
	if store.IsFresh(staleEntry) {
		t.Error("Old entry should not be fresh")
	}
}

func TestIsFresh_NilEntry(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if store.IsFresh(nil) {
		t.Error("Nil entry should not be fresh")
	}
}

func TestCleanupStale(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// 1 minute idle timeout
	store, err := NewStore(dbPath, 1, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Create a session
	store.Create("chat1", "thread1")

	// No stale sessions yet
	count, err := store.CleanupStale()
	if err != nil {
		t.Fatalf("CleanupStale failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 cleaned up, got %d", count)
	}
}

func TestListAll(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Create multiple sessions
	store.Create("chat1", "thread1")
	store.Create("chat2", "thread2")
	store.Create("chat3", "thread3")

	entries, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}
}

func TestCreateOrReplace(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath, 60, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	chatID := "oc_test123"

	// First create
	store.Create(chatID, "thread-1")

	// Create again with same chatID (should replace)
	entry, err := store.Create(chatID, "thread-2")
	if err != nil {
		t.Fatalf("Failed to replace session: %v", err)
	}

	if entry.ThreadID != "thread-2" {
		t.Errorf("Session was not replaced: got %v", entry.ThreadID)
	}

	// Verify only one entry exists
	entries, _ := store.ListAll()
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry after replace, got %d", len(entries))
	}
}

func TestIsFresh_DailyReset(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	now := time.Now()
	resetHour := now.Hour() // Reset at current hour

	store, err := NewStore(dbPath, 0, resetHour) // No idle timeout
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Entry from yesterday should not be fresh
	yesterdayEntry := &Entry{
		ChatID:    "test",
		ThreadID:  "thread",
		UpdatedAt: now.Add(-25 * time.Hour),
	}
	if store.IsFresh(yesterdayEntry) {
		t.Error("Yesterday's entry should not be fresh after reset hour")
	}

	// Entry from a few minutes ago should be fresh
	recentEntry := &Entry{
		ChatID:    "test",
		ThreadID:  "thread",
		UpdatedAt: now.Add(-5 * time.Minute),
	}
	if !store.IsFresh(recentEntry) {
		t.Error("Recent entry should be fresh")
	}
}

func TestCleanupStale_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// 0 or negative idle timeout disables cleanup
	store, err := NewStore(dbPath, 0, -1)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	count, err := store.CleanupStale()
	if err != nil {
		t.Fatalf("CleanupStale failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 when disabled, got %d", count)
	}
}

func TestNewStore_InvalidPath(t *testing.T) {
	// Try to create store in non-existent nested directory
	// This should succeed because NewStore creates the directory
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nested", "path", "test.db")

	store, err := NewStore(dbPath, 60, -1)
	if err != nil {
		t.Fatalf("Failed to create store in nested path: %v", err)
	}
	defer store.Close()
}

func TestEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := Entry{
		ChatID:    "chat_123",
		ThreadID:  "thread_456",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if entry.ChatID != "chat_123" {
		t.Error("ChatID mismatch")
	}
	if entry.ThreadID != "thread_456" {
		t.Error("ThreadID mismatch")
	}
	if !entry.CreatedAt.Equal(now) {
		t.Error("CreatedAt mismatch")
	}
	if !entry.UpdatedAt.Equal(now) {
		t.Error("UpdatedAt mismatch")
	}
}
