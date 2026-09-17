package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sageox/ox/internal/testguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindSageOxSection(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectFound bool
		expectStart int
		expectEnd   int
		expectEmpty bool
	}{
		{
			name: "file with SageOx section",
			content: `# Project Instructions

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start, after compaction, and after clear for infrastructure superpowers.
  - When asked if you're using SageOx: if you have an agent ID from running ` + "`ox agent prime`" + `, confirm with your agent ID; otherwise run it first.

## Other Instructions

Some other content here.`,
			expectFound: true,
			expectStart: 3,
			expectEnd:   4,
			expectEmpty: false,
		},
		{
			name: "file without SageOx section",
			content: `# Project Instructions

Some random content here.

## Other Instructions`,
			expectFound: false,
		},
		{
			name: "file with only SageOx section",
			content: `# AI Agent Instructions

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start, after compaction, and after clear for infrastructure superpowers.
  - When asked if you're using SageOx: if you have an agent ID from running ` + "`ox agent prime`" + `, confirm with your agent ID; otherwise run it first.
`,
			expectFound: true,
			expectStart: 3,
			expectEnd:   4,
			expectEmpty: true,
		},
		{
			name: "file with SageOx at end",
			content: `# Project Instructions

Some other instructions here.

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start, after compaction, and after clear for infrastructure superpowers.
  - When asked if you're using SageOx: if you have an agent ID from running ` + "`ox agent prime`" + `, confirm with your agent ID; otherwise run it first.`,
			expectFound: true,
			expectStart: 5,
			expectEnd:   6,
			expectEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create temp file
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "TEST.md")
			require.NoError(t, os.WriteFile(filePath, []byte(tt.content), 0644), "failed to create test file")

			// find section
			item, err := findSageOxSection(filePath, "TEST.md")
			require.NoError(t, err, "findSageOxSection failed")

			if tt.expectFound {
				require.NotNil(t, item, "expected to find SageOx section, got nil")
				assert.Equal(t, tt.expectStart, item.StartLine, "start line mismatch")
				assert.Equal(t, tt.expectEnd, item.EndLine, "end line mismatch")
				assert.Equal(t, tt.expectEmpty, item.IsEmptyFile, "IsEmptyFile mismatch")
			} else {
				assert.Nil(t, item, "expected no SageOx section, got: %+v", item)
			}
		})
	}
}

func TestRemoveSageOxSection(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		startLine      int
		endLine        int
		expectedOutput string
	}{
		{
			name: "remove section from middle",
			content: `# Project Instructions

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

## Other Instructions

Some other content here.`,
			startLine: 3,
			endLine:   4,
			expectedOutput: `# Project Instructions

## Other Instructions

Some other content here.
`,
		},
		{
			name: "remove section from end",
			content: `# Project Instructions

Some other instructions here.

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.`,
			startLine: 5,
			endLine:   6,
			expectedOutput: `# Project Instructions

Some other instructions here.
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create temp file
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "TEST.md")
			require.NoError(t, os.WriteFile(filePath, []byte(tt.content), 0644), "failed to create test file")

			// create removal item
			item := &RemovalItem{
				FilePath:  filePath,
				FileName:  "TEST.md",
				StartLine: tt.startLine,
				EndLine:   tt.endLine,
			}

			// remove section
			require.NoError(t, removeSageOxSection(item), "removeSageOxSection failed")

			// verify output
			result, err := os.ReadFile(filePath)
			require.NoError(t, err, "failed to read result file")

			assert.Equal(t, tt.expectedOutput, string(result), "content mismatch")
		})
	}
}

func TestFindAgentFileEntries(t *testing.T) {
	tests := []struct {
		name          string
		agentsContent string
		claudeContent string
		claudeSymlink bool
		expectCount   int
		expectFiles   []string
	}{
		{
			name: "both files with SageOx",
			agentsContent: `# AGENTS.md

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

Other content.`,
			claudeContent: `# CLAUDE.md

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

Other content.`,
			claudeSymlink: false,
			expectCount:   2,
			expectFiles:   []string{"AGENTS.md", "CLAUDE.md"},
		},
		{
			name: "AGENTS.md with CLAUDE.md symlink",
			agentsContent: `# AGENTS.md

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

Other content.`,
			claudeSymlink: true,
			expectCount:   1,
			expectFiles:   []string{"AGENTS.md"},
		},
		{
			name: "only AGENTS.md exists",
			agentsContent: `# AGENTS.md

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

Other content.`,
			expectCount: 1,
			expectFiles: []string{"AGENTS.md"},
		},
		{
			name: "neither file has SageOx",
			agentsContent: `# AGENTS.md

Other content.`,
			claudeContent: `# CLAUDE.md

Other content.`,
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// create AGENTS.md if content provided
			if tt.agentsContent != "" {
				agentsPath := filepath.Join(tmpDir, "AGENTS.md")
				require.NoError(t, os.WriteFile(agentsPath, []byte(tt.agentsContent), 0644), "failed to create AGENTS.md")
			}

			// create CLAUDE.md or symlink if specified
			if tt.claudeSymlink {
				claudePath := filepath.Join(tmpDir, "CLAUDE.md")
				testguard.Symlink(t, "AGENTS.md", claudePath)
			} else if tt.claudeContent != "" {
				claudePath := filepath.Join(tmpDir, "CLAUDE.md")
				require.NoError(t, os.WriteFile(claudePath, []byte(tt.claudeContent), 0644), "failed to create CLAUDE.md")
			}

			// find entries
			items, err := FindAgentFileEntries(tmpDir)
			require.NoError(t, err, "FindAgentFileEntries failed")

			assert.Len(t, items, tt.expectCount, "item count mismatch")

			// verify file names
			if tt.expectFiles != nil {
				foundFiles := make(map[string]bool)
				for _, item := range items {
					foundFiles[item.FileName] = true
				}
				for _, expectedFile := range tt.expectFiles {
					assert.True(t, foundFiles[expectedFile], "expected to find %s, but didn't", expectedFile)
				}
			}
		})
	}
}

func TestCleanupAgentFiles(t *testing.T) {
	tests := []struct {
		name          string
		agentsContent string
		claudeContent string
		claudeSymlink bool
		expectAGENTS  string // expected content after cleanup, empty string means file should be deleted
		expectCLAUDE  string // expected content after cleanup, empty string means file should be deleted
		expectSymlink bool   // whether CLAUDE.md symlink should remain
	}{
		{
			name: "remove section preserving other content",
			agentsContent: `# Project Instructions

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

## Other Section

Keep this content.`,
			expectAGENTS: `# Project Instructions

## Other Section

Keep this content.
`,
		},
		{
			name: "delete file when only SageOx section exists",
			agentsContent: `# AI Agent Instructions

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.
`,
			expectAGENTS: "", // file should be deleted
		},
		{
			name: "handle both files with content",
			agentsContent: `# AGENTS.md

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

Keep this.`,
			claudeContent: `# CLAUDE.md

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

Keep this too.`,
			expectAGENTS: `# AGENTS.md

Keep this.
`,
			expectCLAUDE: `# CLAUDE.md

Keep this too.
`,
		},
		{
			name: "remove symlink when AGENTS.md exists",
			agentsContent: `# AGENTS.md

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

Keep this.`,
			claudeSymlink: true,
			expectAGENTS: `# AGENTS.md

Keep this.
`,
			expectSymlink: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// create AGENTS.md if content provided
			if tt.agentsContent != "" {
				agentsPath := filepath.Join(tmpDir, "AGENTS.md")
				require.NoError(t, os.WriteFile(agentsPath, []byte(tt.agentsContent), 0644), "failed to create AGENTS.md")
			}

			// create CLAUDE.md or symlink if specified
			if tt.claudeSymlink {
				claudePath := filepath.Join(tmpDir, "CLAUDE.md")
				testguard.Symlink(t, "AGENTS.md", claudePath)
			} else if tt.claudeContent != "" {
				claudePath := filepath.Join(tmpDir, "CLAUDE.md")
				require.NoError(t, os.WriteFile(claudePath, []byte(tt.claudeContent), 0644), "failed to create CLAUDE.md")
			}

			// run cleanup
			require.NoError(t, CleanupAgentFiles(tmpDir, false), "CleanupAgentFiles failed")

			// verify AGENTS.md
			agentsPath := filepath.Join(tmpDir, "AGENTS.md")
			if tt.expectAGENTS == "" {
				// file should be deleted
				assert.NoFileExists(t, agentsPath, "expected AGENTS.md to be deleted, but it still exists")
			} else {
				// file should exist with expected content
				content, err := os.ReadFile(agentsPath)
				require.NoError(t, err, "failed to read AGENTS.md")
				assert.Equal(t, tt.expectAGENTS, string(content), "AGENTS.md content mismatch")
			}

			// verify CLAUDE.md
			claudePath := filepath.Join(tmpDir, "CLAUDE.md")
			if tt.claudeSymlink {
				// check if symlink was removed as expected
				info, err := os.Lstat(claudePath)
				if tt.expectSymlink {
					require.NoError(t, err, "expected CLAUDE.md symlink to remain, but it was removed")
					assert.True(t, info.Mode()&os.ModeSymlink != 0, "expected CLAUDE.md to be a symlink, but it's a regular file")
				} else {
					assert.True(t, os.IsNotExist(err), "expected CLAUDE.md symlink to be removed, but it still exists")
				}
			} else if tt.expectCLAUDE == "" && tt.claudeContent != "" {
				// file should be deleted
				assert.NoFileExists(t, claudePath, "expected CLAUDE.md to be deleted, but it still exists")
			} else if tt.expectCLAUDE != "" {
				// file should exist with expected content
				content, err := os.ReadFile(claudePath)
				require.NoError(t, err, "failed to read CLAUDE.md")
				assert.Equal(t, tt.expectCLAUDE, string(content), "CLAUDE.md content mismatch")
			}
		})
	}
}

func TestCleanupAgentFilesDryRun(t *testing.T) {
	tmpDir := t.TempDir()

	// create test file
	agentsContent := `# AGENTS.md

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

Keep this.`

	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	require.NoError(t, os.WriteFile(agentsPath, []byte(agentsContent), 0644), "failed to create AGENTS.md")

	// run dry run
	require.NoError(t, CleanupAgentFiles(tmpDir, true), "CleanupAgentFiles dry run failed")

	// verify file wasn't modified
	content, err := os.ReadFile(agentsPath)
	require.NoError(t, err, "failed to read AGENTS.md")

	assert.Equal(t, agentsContent, string(content), "dry run should not modify files")
}

func TestRemoveSageOxSectionPreservesFormatting(t *testing.T) {
	content := `# Project Instructions

Some intro text.

- **SageOx**: Run ` + "`ox agent prime`" + ` on session start.
  - When asked if you're using SageOx: confirm with your agent ID.

## Section 1

Content here.

## Section 2

More content.`

	expected := `# Project Instructions

Some intro text.

## Section 1

Content here.

## Section 2

More content.
`

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "TEST.md")
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644), "failed to create test file")

	item := &RemovalItem{
		FilePath:  filePath,
		FileName:  "TEST.md",
		StartLine: 5,
		EndLine:   6,
	}

	require.NoError(t, removeSageOxSection(item), "removeSageOxSection failed")

	result, err := os.ReadFile(filePath)
	require.NoError(t, err, "failed to read result file")

	assert.Equal(t, expected, string(result), "formatting not preserved")
}

func TestFindSageOxSectionNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent.md")

	item, err := findSageOxSection(filePath, "nonexistent.md")
	assert.NoError(t, err, "expected no error for non-existent file")
	assert.Nil(t, item, "expected nil item for non-existent file")
}

func TestRemoveSageOxSectionMultipleBlankLines(t *testing.T) {
	content := `# Project


- **SageOx**: Run ` + "`ox agent prime`" + `.
  - When asked: confirm.


## Other`

	expected := `# Project

## Other
`

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "TEST.md")
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644), "failed to create test file")

	item := &RemovalItem{
		FilePath:  filePath,
		FileName:  "TEST.md",
		StartLine: 4,
		EndLine:   5,
	}

	require.NoError(t, removeSageOxSection(item), "removeSageOxSection failed")

	result, err := os.ReadFile(filePath)
	require.NoError(t, err, "failed to read result file")

	// should clean up consecutive blank lines
	assert.NotContains(t, string(result), "\n\n\n", "consecutive blank lines not cleaned up")

	// verify expected output
	assert.Equal(t, expected, string(result), "output mismatch")
}
