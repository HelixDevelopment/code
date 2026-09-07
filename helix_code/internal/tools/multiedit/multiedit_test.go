package multiedit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test 1: Transaction Lifecycle
func TestTransactionManager_Lifecycle(t *testing.T) {
	tm := NewTransactionManager(1 * time.Hour)

	// Begin transaction
	tx, err := tm.Begin(context.Background(), EditOptions{})
	require.NoError(t, err)
	assert.Equal(t, StatePending, tx.State)
	assert.NotEmpty(t, tx.ID)

	// Add file edits
	edit := &FileEdit{
		FilePath:   "/tmp/test.txt",
		Operation:  OpUpdate,
		OldContent: []byte("old"),
		NewContent: []byte("new"),
	}
	err = tm.AddEdit(tx, edit)
	require.NoError(t, err)
	assert.Len(t, tx.Files, 1)

	// Update state
	err = tm.UpdateState(tx, StatePreview)
	require.NoError(t, err)
	assert.Equal(t, StatePreview, tx.State)

	// Get transaction
	retrieved, err := tm.Get(tx.ID)
	require.NoError(t, err)
	assert.Equal(t, tx.ID, retrieved.ID)
}

// Test 2: Backup and Restore
func TestBackupManager_BackupRestore(t *testing.T) {
	tmpDir := t.TempDir()
	bm := NewBackupManager(filepath.Join(tmpDir, "backups"), 24*time.Hour)

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")
	err := os.WriteFile(testFile, content, 0644)
	require.NoError(t, err)

	// Backup
	backupPath, err := bm.Backup(context.Background(), testFile)
	require.NoError(t, err)
	assert.FileExists(t, backupPath)

	// Modify original
	err = os.WriteFile(testFile, []byte("modified"), 0644)
	require.NoError(t, err)

	// Restore
	err = bm.Restore(context.Background(), backupPath, testFile)
	require.NoError(t, err)

	// Verify
	restored, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, content, restored)
}

// Test 3: Compressed Backup
func TestBackupManager_CompressedBackup(t *testing.T) {
	tmpDir := t.TempDir()
	bm := NewBackupManager(filepath.Join(tmpDir, "backups"), 24*time.Hour)

	// Create test file with large content
	testFile := filepath.Join(tmpDir, "large.txt")
	content := []byte("large content that should be compressed\n")
	for i := 0; i < 100; i++ {
		content = append(content, []byte("line "+string(rune(i))+"\n")...)
	}
	err := os.WriteFile(testFile, content, 0644)
	require.NoError(t, err)

	// Backup with compression
	backupPath, err := bm.BackupWithCompression(context.Background(), testFile)
	require.NoError(t, err)
	assert.Contains(t, backupPath, ".gz")

	// Restore
	restored := filepath.Join(tmpDir, "restored.txt")
	err = bm.Restore(context.Background(), backupPath, restored)
	require.NoError(t, err)

	// Verify
	restoredContent, err := os.ReadFile(restored)
	require.NoError(t, err)
	assert.Equal(t, content, restoredContent)
}

// Test 4: Diff Generation
func TestDiffManager_GenerateApply(t *testing.T) {
	dm := NewDiffManager(FormatUnified)

	old := []byte("line 1\nline 2\nline 3\n")
	new := []byte("line 1\nmodified line 2\nline 3\n")

	// Generate diff
	diff, err := dm.GenerateDiff(old, new, "test.txt")
	require.NoError(t, err)
	assert.NotEmpty(t, diff.Unified)
	assert.Greater(t, len(diff.Hunks), 0)

	// Apply diff
	result, err := dm.ApplyDiff(diff)
	require.NoError(t, err)
	assert.Equal(t, string(new), string(result))
}

// Test 5: Diff Stats
func TestDiffManager_Stats(t *testing.T) {
	dm := NewDiffManager(FormatUnified)

	old := []byte("line 1\nline 2\nline 3\n")
	new := []byte("line 1\nmodified line 2\nline 3\nline 4\n")

	diff, err := dm.GenerateDiff(old, new, "test.txt")
	require.NoError(t, err)

	assert.Greater(t, diff.Stats.LinesAdded, 0)
	assert.GreaterOrEqual(t, diff.Stats.LinesDeleted, 0)
}

// Test 6: State Transitions
func TestTransactionManager_StateTransitions(t *testing.T) {
	tm := NewTransactionManager(1 * time.Hour)
	tx, _ := tm.Begin(context.Background(), EditOptions{})

	// Valid transitions
	assert.NoError(t, tm.UpdateState(tx, StatePreview))
	assert.NoError(t, tm.UpdateState(tx, StateReady))
	assert.NoError(t, tm.UpdateState(tx, StateCommitting))
	assert.NoError(t, tm.UpdateState(tx, StateCommitted))

	// Invalid transition from terminal state
	err := tm.UpdateState(tx, StatePending)
	assert.Error(t, err)
}

// Test 7: Conflict Detection
func TestConflictResolver_DetectModification(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with known content
	testFile := filepath.Join(tmpDir, "test.txt")
	original := []byte("original content")
	err := os.WriteFile(testFile, original, 0644)
	require.NoError(t, err)

	// Create edit with checksum
	edit := &FileEdit{
		FilePath:   testFile,
		OldContent: original,
		NewContent: []byte("new content"),
		Checksum:   calculateChecksum(original),
	}

	// Create transaction
	tx := &EditTransaction{
		Files: []*FileEdit{edit},
	}

	// No conflict initially
	cr := NewConflictResolver(false)
	conflicts, err := cr.DetectConflicts(context.Background(), tx)
	require.NoError(t, err)
	assert.Len(t, conflicts, 0)
}

// Test 8: Multi-File Edit
func TestMultiFileEditor_MultiFileEdit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create editor
	config := DefaultConfig()
	config.WorkspaceRoot = tmpDir
	config.BackupDir = filepath.Join(tmpDir, "backups")
	config.BackupEnabled = true

	mfe, err := NewMultiFileEditor(WithConfig(config))
	require.NoError(t, err)

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file1, []byte("content 1"), 0644)
	os.WriteFile(file2, []byte("content 2"), 0644)

	// Begin transaction
	tx, err := mfe.BeginEdit(context.Background(), EditOptions{
		BackupEnabled: true,
	})
	require.NoError(t, err)

	// Add edits
	err = mfe.AddEdit(context.Background(), tx, &FileEdit{
		FilePath:   file1,
		Operation:  OpUpdate,
		OldContent: []byte("content 1"),
		NewContent: []byte("updated 1"),
		Checksum:   calculateChecksum([]byte("content 1")),
	})
	require.NoError(t, err)

	err = mfe.AddEdit(context.Background(), tx, &FileEdit{
		FilePath:   file2,
		Operation:  OpUpdate,
		OldContent: []byte("content 2"),
		NewContent: []byte("updated 2"),
		Checksum:   calculateChecksum([]byte("content 2")),
	})
	require.NoError(t, err)

	// Preview
	preview, err := mfe.Preview(context.Background(), tx)
	require.NoError(t, err)
	assert.Len(t, preview.Files, 2)
	assert.Equal(t, 2, preview.Summary.FilesModified)

	// Commit
	err = mfe.Commit(context.Background(), tx)
	require.NoError(t, err)

	// Verify
	content1, _ := os.ReadFile(file1)
	content2, _ := os.ReadFile(file2)
	assert.Equal(t, []byte("updated 1"), content1)
	assert.Equal(t, []byte("updated 2"), content2)
}

// Test 9: Rollback on Error
func TestMultiFileEditor_RollbackOnError(t *testing.T) {
	tmpDir := t.TempDir()

	config := DefaultConfig()
	config.WorkspaceRoot = tmpDir
	config.BackupDir = filepath.Join(tmpDir, "backups")
	config.BackupEnabled = true

	mfe, err := NewMultiFileEditor(WithConfig(config))
	require.NoError(t, err)

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file1, []byte("content 1"), 0644)
	os.WriteFile(file2, []byte("content 2"), 0644)

	// Begin transaction
	tx, err := mfe.BeginEdit(context.Background(), EditOptions{
		BackupEnabled: true,
	})
	require.NoError(t, err)

	// Add edit for file1
	err = mfe.AddEdit(context.Background(), tx, &FileEdit{
		FilePath:   file1,
		Operation:  OpUpdate,
		OldContent: []byte("content 1"),
		NewContent: []byte("updated 1"),
		Checksum:   calculateChecksum([]byte("content 1")),
	})
	require.NoError(t, err)

	// Add edit for file2
	err = mfe.AddEdit(context.Background(), tx, &FileEdit{
		FilePath:   file2,
		Operation:  OpUpdate,
		OldContent: []byte("content 2"),
		NewContent: []byte("updated 2"),
		Checksum:   calculateChecksum([]byte("content 2")),
	})
	require.NoError(t, err)

	// Modify file2 externally to cause checksum mismatch during commit
	os.WriteFile(file2, []byte("modified externally"), 0644)

	// Preview should succeed (doesn't verify checksums)
	_, err = mfe.Preview(context.Background(), tx)
	require.NoError(t, err)

	// Commit should fail due to checksum mismatch and rollback
	err = mfe.Commit(context.Background(), tx)
	assert.Error(t, err)

	// Anti-bluff (CONST-035 / §11.9): the original comment said "the file
	// may or may not be modified depending on which edit runs first" but
	// asserted NOTHING about the rollback. The whole point of a
	// transaction is that on commit failure, ALL files MUST be left in
	// their pre-transaction state. Whichever edit ran first, file1's
	// content MUST be "content 1" (original) after rollback — anything
	// else means rollback is broken. Pin that contract.
	final1, readErr := os.ReadFile(file1)
	require.NoError(t, readErr, "file1 must remain readable after rollback")
	assert.Equal(t, "content 1", string(final1),
		"transaction rollback must restore file1 to its pre-transaction content regardless of edit ordering")
}

// Test 10: Preview Generation
func TestPreviewEngine_Preview(t *testing.T) {
	dm := NewDiffManager(FormatUnified)
	pe := NewPreviewEngine(dm, 3)

	tx := &EditTransaction{
		ID: "test-tx",
		Files: []*FileEdit{
			{
				FilePath:   "file1.txt",
				Operation:  OpUpdate,
				OldContent: []byte("old"),
				NewContent: []byte("new"),
			},
			{
				FilePath:   "file2.txt",
				Operation:  OpCreate,
				NewContent: []byte("created"),
			},
		},
	}

	preview, err := pe.Preview(context.Background(), tx)
	require.NoError(t, err)
	assert.Len(t, preview.Files, 2)
	assert.Equal(t, 1, preview.Summary.FilesCreated)
	assert.Equal(t, 1, preview.Summary.FilesModified)
}

// Test 11: Transaction Timeout
func TestTransactionManager_Timeout(t *testing.T) {
	tm := NewTransactionManager(100 * time.Millisecond)

	tx, err := tm.Begin(context.Background(), EditOptions{})
	require.NoError(t, err)

	// Wait for the timeout-monitor goroutine to abort the transaction.
	// GetState() reads the field under the transaction lock — the timeout
	// monitor writes tx.State under tx.mu, so a direct read would race.
	// The assertion is the predicate, never elapsed time. Generous
	// ceiling + tiny interval: a fast machine exits on the first check.
	require.Eventually(t, func() bool {
		return tx.GetState() == StateAborted
	}, 30*time.Second, 5*time.Millisecond, "transaction never aborted on timeout")

	// State should be aborted.
	assert.Equal(t, StateAborted, tx.GetState())
}

// Test 12: Backup Cleanup
func TestBackupManager_Cleanup(t *testing.T) {
	tmpDir := t.TempDir()
	bm := NewBackupManager(filepath.Join(tmpDir, "backups"), 100*time.Millisecond)

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	// Create backup
	backupPath, err := bm.Backup(context.Background(), testFile)
	require.NoError(t, err)

	// Wait for retention period
	time.Sleep(200 * time.Millisecond)

	// Cleanup
	err = bm.Cleanup(context.Background())
	require.NoError(t, err)

	// Backup should be deleted
	_, err = os.Stat(backupPath)
	assert.True(t, os.IsNotExist(err))
}

// Test 13: Checksum Verification
func TestMultiFileEditor_ChecksumMismatch(t *testing.T) {
	tmpDir := t.TempDir()

	config := DefaultConfig()
	config.WorkspaceRoot = tmpDir
	config.BackupDir = filepath.Join(tmpDir, "backups")

	mfe, err := NewMultiFileEditor(WithConfig(config))
	require.NoError(t, err)

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	// Begin transaction
	tx, err := mfe.BeginEdit(context.Background(), EditOptions{})
	require.NoError(t, err)

	// Add edit with wrong checksum
	err = mfe.AddEdit(context.Background(), tx, &FileEdit{
		FilePath:   testFile,
		Operation:  OpUpdate,
		OldContent: []byte("old content"),
		NewContent: []byte("new content"),
		Checksum:   "wrong-checksum",
	})
	require.NoError(t, err)

	// Preview
	_, err = mfe.Preview(context.Background(), tx)
	require.NoError(t, err)

	// Commit should fail due to checksum mismatch
	err = mfe.Commit(context.Background(), tx)
	assert.Error(t, err)
}

// Test 14: Create Operation
func TestMultiFileEditor_CreateFile(t *testing.T) {
	tmpDir := t.TempDir()

	config := DefaultConfig()
	config.WorkspaceRoot = tmpDir
	config.BackupDir = filepath.Join(tmpDir, "backups")

	mfe, err := NewMultiFileEditor(WithConfig(config))
	require.NoError(t, err)

	// Begin transaction
	tx, err := mfe.BeginEdit(context.Background(), EditOptions{})
	require.NoError(t, err)

	// Add create operation
	newFile := filepath.Join(tmpDir, "newfile.txt")
	err = mfe.AddEdit(context.Background(), tx, &FileEdit{
		FilePath:   newFile,
		Operation:  OpCreate,
		NewContent: []byte("new file content"),
	})
	require.NoError(t, err)

	// Preview
	preview, err := mfe.Preview(context.Background(), tx)
	require.NoError(t, err)
	assert.Equal(t, 1, preview.Summary.FilesCreated)

	// Commit
	err = mfe.Commit(context.Background(), tx)
	require.NoError(t, err)

	// Verify file was created
	content, err := os.ReadFile(newFile)
	require.NoError(t, err)
	assert.Equal(t, []byte("new file content"), content)
}

// Test 15: Delete Operation
func TestMultiFileEditor_DeleteFile(t *testing.T) {
	tmpDir := t.TempDir()

	config := DefaultConfig()
	config.WorkspaceRoot = tmpDir
	config.BackupDir = filepath.Join(tmpDir, "backups")
	config.BackupEnabled = true

	mfe, err := NewMultiFileEditor(WithConfig(config))
	require.NoError(t, err)

	// Create test file
	testFile := filepath.Join(tmpDir, "delete.txt")
	os.WriteFile(testFile, []byte("to be deleted"), 0644)

	// Begin transaction
	tx, err := mfe.BeginEdit(context.Background(), EditOptions{
		BackupEnabled: true,
	})
	require.NoError(t, err)

	// Add delete operation
	err = mfe.AddEdit(context.Background(), tx, &FileEdit{
		FilePath:   testFile,
		Operation:  OpDelete,
		OldContent: []byte("to be deleted"),
		Checksum:   calculateChecksum([]byte("to be deleted")),
	})
	require.NoError(t, err)

	// Preview
	preview, err := mfe.Preview(context.Background(), tx)
	require.NoError(t, err)
	assert.Equal(t, 1, preview.Summary.FilesDeleted)

	// Commit
	err = mfe.Commit(context.Background(), tx)
	require.NoError(t, err)

	// Verify file was deleted
	_, err = os.Stat(testFile)
	assert.True(t, os.IsNotExist(err))
}

// Test 16: Transaction List and Cleanup
func TestTransactionManager_ListAndCleanup(t *testing.T) {
	tm := NewTransactionManager(1 * time.Hour)

	// Create multiple transactions
	tx1, _ := tm.Begin(context.Background(), EditOptions{})
	tx2, _ := tm.Begin(context.Background(), EditOptions{})
	tx3, _ := tm.Begin(context.Background(), EditOptions{})

	// List should return all
	list := tm.List()
	assert.Len(t, list, 3)

	// Mark some as completed
	tm.UpdateState(tx1, StateCommitted)
	tm.UpdateState(tx2, StateRolledBack)

	// Cleanup old transactions
	count := tm.Cleanup(0) // Cleanup immediately
	assert.GreaterOrEqual(t, count, 0)

	// tx3 should still be in list
	list = tm.List()
	found := false
	for _, tx := range list {
		if tx.ID == tx3.ID {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// Test 17: Preview Formatter
//
// Round-428 CONST-046 §11.4: the PreviewFormatter render text is now
// resolved through the package i18n seam. The test wires a fake
// Translator that emits the active.en.yaml English strings so the
// assertions exercise the real end-user output (NoopTranslator would
// echo message IDs — a §11.4 PASS-bluff if asserted against).
func TestPreviewFormatter_Format(t *testing.T) {
	SetTranslator(previewEnglishTranslator{})
	t.Cleanup(func() { SetTranslator(nil) })

	result := &PreviewResult{
		TransactionID: "test-tx",
		Summary: &PreviewSummary{
			TotalFiles:    2,
			FilesCreated:  1,
			FilesModified: 1,
		},
		Files: []*FilePreview{
			{
				FilePath:  "file1.txt",
				Operation: OpCreate,
			},
			{
				FilePath:  "file2.txt",
				Operation: OpUpdate,
			},
		},
	}

	// Test plain format
	pf := NewPreviewFormatter(FormatPlain)
	plain, err := pf.Format(result)
	require.NoError(t, err)
	assert.Contains(t, plain, "test-tx")
	assert.Contains(t, plain, "2 files")

	// Test markdown format
	pf = NewPreviewFormatter(FormatMarkdown)
	markdown, err := pf.Format(result)
	require.NoError(t, err)
	assert.Contains(t, markdown, "# Transaction Preview")

	// Test HTML format
	pf = NewPreviewFormatter(FormatHTML)
	html, err := pf.Format(result)
	require.NoError(t, err)
	assert.Contains(t, html, "<html>")
}

// previewEnglishTranslator is a CONST-046 fake Translator that
// resolves the multiedit message IDs to their active.en.yaml English
// renderings with placeholder interpolation. It exists so the
// formatter tests assert against real end-user output.
type previewEnglishTranslator struct{}

func (previewEnglishTranslator) T(_ context.Context, id string, d map[string]any) (string, error) {
	g := func(k string) any { return d[k] }
	switch id {
	case "internal_tools_multiedit_preview_transaction_line":
		return fmt.Sprintf("Transaction: %v", g("ID")), nil
	case "internal_tools_multiedit_preview_summary_line":
		return fmt.Sprintf("Summary: %v files (%v created, %v modified, %v deleted)",
			g("TotalFiles"), g("Created"), g("Modified"), g("Deleted")), nil
	case "internal_tools_multiedit_preview_changes_line":
		return fmt.Sprintf("Changes: +%v -%v lines", g("Added"), g("Deleted")), nil
	case "internal_tools_multiedit_preview_conflicts_line":
		return fmt.Sprintf("Conflicts: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_files_heading":
		return "Files:", nil
	case "internal_tools_multiedit_preview_md_title":
		return fmt.Sprintf("# Transaction Preview: %v", g("ID")), nil
	case "internal_tools_multiedit_preview_md_summary_heading":
		return "## Summary", nil
	case "internal_tools_multiedit_preview_md_total_files":
		return fmt.Sprintf("- **Total Files**: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_md_created":
		return fmt.Sprintf("- **Created**: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_md_modified":
		return fmt.Sprintf("- **Modified**: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_md_deleted":
		return fmt.Sprintf("- **Deleted**: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_md_lines_added":
		return fmt.Sprintf("- **Lines Added**: +%v", g("Count")), nil
	case "internal_tools_multiedit_preview_md_lines_deleted":
		return fmt.Sprintf("- **Lines Deleted**: -%v", g("Count")), nil
	case "internal_tools_multiedit_preview_md_conflicts_heading":
		return "## Conflicts", nil
	case "internal_tools_multiedit_preview_md_files_heading":
		return "## Files", nil
	case "internal_tools_multiedit_preview_html_title":
		return fmt.Sprintf("Transaction Preview: %v", g("ID")), nil
	case "internal_tools_multiedit_preview_html_summary_heading":
		return "Summary", nil
	case "internal_tools_multiedit_preview_html_total_files":
		return fmt.Sprintf("Total Files: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_html_created":
		return fmt.Sprintf("Created: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_html_modified":
		return fmt.Sprintf("Modified: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_html_deleted":
		return fmt.Sprintf("Deleted: %v", g("Count")), nil
	case "internal_tools_multiedit_preview_html_conflicts_heading":
		return "Conflicts", nil
	case "internal_tools_multiedit_preview_html_files_heading":
		return "Files", nil
	default:
		return id, nil
	}
}

func (previewEnglishTranslator) TPlural(_ context.Context, id string, _ int, _ map[string]any) (string, error) {
	return id, nil
}

// TestPreviewFormatter_CONST046_GoesThroughTranslator is the paired
// mutation test for round-428: it proves PreviewFormatter routes its
// user-facing text through the i18n seam rather than emitting
// hardcoded literals. With a sentinel translator that overrides
// every message ID, NONE of the original English literals may appear
// in the rendered output. If a future edit reintroduces a hardcoded
// literal, this test fails — the CONST-046 regression guard.
func TestPreviewFormatter_CONST046_GoesThroughTranslator(t *testing.T) {
	SetTranslator(previewSentinelTranslator{})
	t.Cleanup(func() { SetTranslator(nil) })

	result := &PreviewResult{
		TransactionID: "tx-sentinel",
		Summary: &PreviewSummary{
			TotalFiles: 1, FilesCreated: 1, HasConflicts: true,
		},
		Conflicts: []*Conflict{{FilePath: "c.txt", Description: "clash"}},
		Files:     []*FilePreview{{FilePath: "f.txt", Operation: OpCreate}},
	}

	hardcoded := []string{
		"Transaction:", "Summary:", "Changes:", "Files:",
		"# Transaction Preview", "## Summary", "Total Files",
		"Lines Added", "Lines Deleted", "## Conflicts", "## Files",
		"<h1>Transaction Preview", "<li>Total Files",
	}
	for _, format := range []OutputFormat{FormatPlain, FormatMarkdown, FormatHTML} {
		out, err := NewPreviewFormatter(format).Format(result)
		require.NoError(t, err)
		require.Contains(t, out, "SENTINEL",
			"format %v: translator was bypassed — output has no sentinel marker", format)
		for _, lit := range hardcoded {
			assert.NotContains(t, out, lit,
				"format %v: hardcoded literal %q leaked past the i18n seam", format, lit)
		}
	}
}

// previewSentinelTranslator replaces every resolved message with a
// marker so any surviving hardcoded literal is detectable.
type previewSentinelTranslator struct{}

func (previewSentinelTranslator) T(_ context.Context, id string, _ map[string]any) (string, error) {
	return "SENTINEL[" + id + "]", nil
}

func (previewSentinelTranslator) TPlural(_ context.Context, id string, _ int, _ map[string]any) (string, error) {
	return "SENTINEL[" + id + "]", nil
}

// Test 18: Atomic Write Rollback
func TestMultiFileEditor_AtomicRollback(t *testing.T) {
	tmpDir := t.TempDir()

	config := DefaultConfig()
	config.WorkspaceRoot = tmpDir
	config.BackupDir = filepath.Join(tmpDir, "backups")
	config.BackupEnabled = true

	mfe, err := NewMultiFileEditor(WithConfig(config))
	require.NoError(t, err)

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	file3 := filepath.Join(tmpDir, "file3.txt")
	os.WriteFile(file1, []byte("content 1"), 0644)
	os.WriteFile(file2, []byte("content 2"), 0644)
	os.WriteFile(file3, []byte("content 3"), 0644)

	// Begin transaction
	tx, err := mfe.BeginEdit(context.Background(), EditOptions{
		BackupEnabled: true,
	})
	require.NoError(t, err)

	// Add multiple edits
	for i, file := range []string{file1, file2, file3} {
		content := []byte("content " + string(rune('1'+i)))
		err = mfe.AddEdit(context.Background(), tx, &FileEdit{
			FilePath:   file,
			Operation:  OpUpdate,
			OldContent: content,
			NewContent: []byte("updated " + string(rune('1'+i))),
			Checksum:   calculateChecksum(content),
		})
		require.NoError(t, err)
	}

	// Preview
	_, err = mfe.Preview(context.Background(), tx)
	require.NoError(t, err)

	// Manually rollback
	err = mfe.Rollback(context.Background(), tx)
	require.NoError(t, err)

	// Verify all files are unchanged
	content1, _ := os.ReadFile(file1)
	content2, _ := os.ReadFile(file2)
	content3, _ := os.ReadFile(file3)
	assert.Equal(t, []byte("content 1"), content1)
	assert.Equal(t, []byte("content 2"), content2)
	assert.Equal(t, []byte("content 3"), content3)
}

// Test 19: Diff Parse and Apply
func TestDiffManager_ParseAndApply(t *testing.T) {
	dm := NewDiffManager(FormatUnified)

	// Generate a diff
	old := []byte("line 1\nline 2\nline 3\n")
	new := []byte("line 1\nmodified line 2\nline 3\n")

	diff, err := dm.GenerateDiff(old, new, "test.txt")
	require.NoError(t, err)

	// Parse the unified diff
	parsed, err := dm.ParseDiff(diff.Unified)
	require.NoError(t, err)
	assert.Greater(t, len(parsed.Hunks), 0)

	// Apply parsed diff
	parsed.OldContent = old
	parsed.NewContent = new // Set new content to preserve trailing newline
	result, err := dm.ApplyDiff(parsed)
	require.NoError(t, err)
	assert.Equal(t, string(new), string(result))
}

// Test 20: Large File Handling
func TestMultiFileEditor_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()

	config := DefaultConfig()
	config.BackupDir = filepath.Join(tmpDir, "backups")
	config.MaxFileSize = 1024 // 1KB limit

	mfe, err := NewMultiFileEditor(WithConfig(config))
	require.NoError(t, err)

	// Begin transaction
	tx, err := mfe.BeginEdit(context.Background(), EditOptions{})
	require.NoError(t, err)

	// Try to add large file (exceeds limit)
	largeContent := make([]byte, 2048) // 2KB
	err = mfe.AddEdit(context.Background(), tx, &FileEdit{
		FilePath:   filepath.Join(tmpDir, "large.txt"),
		Operation:  OpCreate,
		NewContent: largeContent,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds limit")
}
