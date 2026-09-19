package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindSageoxHooks(t *testing.T) {
	tests := []struct {
		name      string
		hookFiles map[string]string // filename -> content
		want      []string          // expected hook names found
	}{
		{
			name: "no hooks directory",
			want: nil,
		},
		{
			name:      "empty hooks directory",
			hookFiles: map[string]string{
				// empty
			},
			want: nil,
		},
		{
			name: "only sample files",
			hookFiles: map[string]string{
				"pre-commit.sample":     "#!/bin/sh\n# git sample hook\necho 'sample'\n",
				"prepare-commit.sample": "#!/bin/sh\necho 'sample'\n",
			},
			want: nil,
		},
		{
			name: "SageOx pre-commit hook only",
			hookFiles: map[string]string{
				"pre-commit": "#!/bin/sh\n# ox pre-commit hook\necho 'lint'\n",
			},
			want: []string{"pre-commit"},
		},
		{
			name: "SageOx hook with beads marker",
			hookFiles: map[string]string{
				"pre-commit": "#!/bin/sh\n# Some hook\n# bd-hooks-version: 0.29.0\nbd sync\n",
			},
			want: []string{"pre-commit"},
		},
		{
			name: "mixed SageOx and non-SageOx hooks",
			hookFiles: map[string]string{
				"pre-commit":  "#!/bin/sh\n# ox pre-commit hook\necho 'ox'\n",
				"post-commit": "#!/bin/sh\n# husky\necho 'husky'\n",
				"commit-msg":  "#!/bin/sh\n# SageOx hook\necho 'sageox'\n",
			},
			want: []string{"pre-commit", "commit-msg"},
		},
		{
			name: "non-SageOx hooks only",
			hookFiles: map[string]string{
				"pre-commit":  "#!/bin/sh\n# husky\necho 'husky'\n",
				"post-commit": "#!/bin/sh\n# lefthook\necho 'left'\n",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create temp repo
			tmpDir := t.TempDir()
			hooksDir := filepath.Join(tmpDir, ".git", "hooks")

			// create hooks directory and files if needed
			if tt.hookFiles != nil {
				err := os.MkdirAll(hooksDir, 0755)
				require.NoError(t, err)

				for name, content := range tt.hookFiles {
					path := filepath.Join(hooksDir, name)
					err := os.WriteFile(path, []byte(content), 0755)
					require.NoError(t, err)
				}
			}

			got, err := FindSageoxHooks(tmpDir)
			require.NoError(t, err, "FindSageoxHooks() error")

			var gotNames []string
			for _, item := range got {
				gotNames = append(gotNames, item.Name)
			}

			assert.Len(t, gotNames, len(tt.want), "FindSageoxHooks() found %d hooks, want %d\ngot: %v\nwant: %v",
				len(gotNames), len(tt.want), gotNames, tt.want)

			// check each expected hook is found
			for _, wantName := range tt.want {
				assert.Contains(t, gotNames, wantName, "FindSageoxHooks() missing expected hook %s", wantName)
			}
		})
	}
}

func TestAnalyzeHookFile(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantSageox    bool
		wantMixed     bool
		wantEntireSox bool
	}{
		{
			name:          "pure SageOx hook",
			content:       "#!/bin/sh\n# ox pre-commit hook\n# Runs linting\nset -e\nmake lint\n",
			wantSageox:    true,
			wantMixed:     false,
			wantEntireSox: true,
		},
		{
			name:          "SageOx hook with beads",
			content:       "#!/bin/sh\n# ox pre-commit hook\n# bd-hooks-version: 0.29.0\nbd sync\n",
			wantSageox:    true,
			wantMixed:     false,
			wantEntireSox: true,
		},
		{
			name: "mixed SageOx and husky",
			content: `#!/bin/sh
# husky
npm test

# ox pre-commit hook
make lint

# bd-hooks-version: 0.29.0
bd sync
`,
			wantSageox:    true,
			wantMixed:     true,
			wantEntireSox: false,
		},
		{
			name:          "non-SageOx hook",
			content:       "#!/bin/sh\n# husky\nnpm test\n",
			wantSageox:    false,
			wantMixed:     false,
			wantEntireSox: false,
		},
		{
			name:          "empty file",
			content:       "",
			wantSageox:    false,
			wantMixed:     false,
			wantEntireSox: false,
		},
		{
			name:          "just shebang",
			content:       "#!/bin/sh\n",
			wantSageox:    false,
			wantMixed:     false,
			wantEntireSox: false,
		},
		{
			name: "SageOx with lefthook",
			content: `#!/bin/sh
# ox pre-commit hook
make lint

# lefthook
npm test
`,
			wantSageox:    true,
			wantMixed:     true,
			wantEntireSox: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// write content to temp file
			tmpFile := filepath.Join(t.TempDir(), "hook")
			err := os.WriteFile(tmpFile, []byte(tt.content), 0755)
			require.NoError(t, err)

			gotSageox, gotMixed, gotEntireSox, err := analyzeHookFile(tmpFile)
			require.NoError(t, err, "analyzeHookFile() error")

			assert.Equal(t, tt.wantSageox, gotSageox, "analyzeHookFile() hasSageox")
			assert.Equal(t, tt.wantMixed, gotMixed, "analyzeHookFile() hasMixed")
			assert.Equal(t, tt.wantEntireSox, gotEntireSox, "analyzeHookFile() isEntireSox")
		})
	}
}

func TestRemoveRepoHooks(t *testing.T) {
	tests := []struct {
		name       string
		hookFiles  map[string]string // filename -> content
		dryRun     bool
		wantRemain map[string]bool // hooks that should remain -> true if should exist
		wantErr    bool
	}{
		{
			name:   "no hooks to remove",
			dryRun: false,
			hookFiles: map[string]string{
				"pre-commit": "#!/bin/sh\n# husky\nnpm test\n",
			},
			wantRemain: map[string]bool{
				"pre-commit": true,
			},
		},
		{
			name:   "remove entire SageOx hook",
			dryRun: false,
			hookFiles: map[string]string{
				"pre-commit": "#!/bin/sh\n# ox pre-commit hook\nmake lint\n",
			},
			wantRemain: map[string]bool{
				"pre-commit": false,
			},
		},
		{
			name:   "dry run - don't actually remove",
			dryRun: true,
			hookFiles: map[string]string{
				"pre-commit": "#!/bin/sh\n# ox pre-commit hook\nmake lint\n",
			},
			wantRemain: map[string]bool{
				"pre-commit": true, // should still exist in dry run
			},
		},
		{
			name:   "remove SageOx sections from mixed hook",
			dryRun: false,
			hookFiles: map[string]string{
				"pre-commit": "#!/bin/sh\n# husky\nnpm test\n\n# ox pre-commit hook\nmake lint\n",
			},
			wantRemain: map[string]bool{
				"pre-commit": true, // should exist but cleaned
			},
		},
		{
			name:   "remove beads section",
			dryRun: false,
			hookFiles: map[string]string{
				"pre-commit": "#!/bin/sh\n# husky\nnpm test\n\n# bd-hooks-version: 0.29.0\nbd sync\n",
			},
			wantRemain: map[string]bool{
				"pre-commit": true, // should exist but cleaned
			},
		},
		{
			name:   "multiple hooks - mixed removal",
			dryRun: false,
			hookFiles: map[string]string{
				"pre-commit":  "#!/bin/sh\n# ox pre-commit hook\nmake lint\n",
				"post-commit": "#!/bin/sh\n# husky\nnpm test\n",
				"commit-msg":  "#!/bin/sh\n# SageOx hook\necho 'msg'\n# husky\necho 'husky'\n",
			},
			wantRemain: map[string]bool{
				"pre-commit":  false, // entirely SageOx - deleted
				"post-commit": true,  // no SageOx - preserved
				"commit-msg":  true,  // mixed - cleaned but preserved
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create temp repo
			tmpDir := t.TempDir()
			hooksDir := filepath.Join(tmpDir, ".git", "hooks")

			err := os.MkdirAll(hooksDir, 0755)
			require.NoError(t, err)

			// create hook files
			for name, content := range tt.hookFiles {
				path := filepath.Join(hooksDir, name)
				err := os.WriteFile(path, []byte(content), 0755)
				require.NoError(t, err)
			}

			// remove hooks
			err = RemoveRepoHooks(tmpDir, tt.dryRun)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// check which hooks remain
			for hookName, shouldExist := range tt.wantRemain {
				hookPath := filepath.Join(hooksDir, hookName)
				_, err := os.Stat(hookPath)
				exists := err == nil

				assert.Equal(t, shouldExist, exists, "hook %s: exists = %v, want %v", hookName, exists, shouldExist)

				// if hook should exist and was mixed, verify SageOx content removed
				// SKIP this check for dry run tests - dry run doesn't modify files
				if !tt.dryRun && exists && shouldExist && strings.Contains(tt.hookFiles[hookName], "ox") {
					content, err := os.ReadFile(hookPath)
					require.NoError(t, err)

					// check that SageOx markers are gone
					contentStr := string(content)
					for _, marker := range sageoxHookMarkers {
						assert.NotContains(t, contentStr, marker, "hook %s still contains SageOx marker: %s", hookName, marker)
					}

					// check that beads marker is gone
					assert.NotContains(t, contentStr, beadsHookMarker, "hook %s still contains beads marker", hookName)
				}
			}
		})
	}
}

func TestRemoveSageoxSections(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain []string // strings that should be in output
		wantNotIn   []string // strings that should NOT be in output
	}{
		{
			name: "remove ox section from mixed hook",
			input: `#!/bin/sh
# husky
npm test

# ox pre-commit hook
make lint

# more user content
echo "done"
`,
			wantContain: []string{
				"#!/bin/sh",
				"# husky",
				"npm test",
				"# more user content",
				"echo \"done\"",
			},
			wantNotIn: []string{
				"# ox pre-commit hook",
				"make lint",
			},
		},
		{
			name: "remove beads section",
			input: `#!/bin/sh
# user hook
echo "start"

# bd-hooks-version: 0.29.0
bd sync --flush-only
git add .beads/beads.jsonl

# more user content
echo "end"
`,
			wantContain: []string{
				"#!/bin/sh",
				"# user hook",
				"echo \"start\"",
				"# more user content",
				"echo \"end\"",
			},
			wantNotIn: []string{
				"bd-hooks-version",
				"bd sync",
				"git add .beads",
			},
		},
		{
			name: "preserve other tool markers",
			input: `#!/bin/sh
# husky
npm test

# ox pre-commit hook
make lint

# lefthook
echo "lefthook"
`,
			wantContain: []string{
				"#!/bin/sh",
				"# husky",
				"npm test",
				"# lefthook",
				"echo \"lefthook\"",
			},
			wantNotIn: []string{
				"# ox pre-commit hook",
				"make lint",
			},
		},
		{
			name: "multiple SageOx sections",
			input: `#!/bin/sh
# user content
echo "user"

# ox pre-commit hook
make lint

# middle content
echo "middle"

# SageOx hook
ox prime

# end content
echo "end"
`,
			wantContain: []string{
				"#!/bin/sh",
				"# user content",
				"echo \"user\"",
				"# middle content",
				"echo \"middle\"",
				"# end content",
				"echo \"end\"",
			},
			wantNotIn: []string{
				"# ox pre-commit hook",
				"make lint",
				"# SageOx hook",
				"ox prime",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// write input to temp file
			tmpFile := filepath.Join(t.TempDir(), "hook")
			err := os.WriteFile(tmpFile, []byte(tt.input), 0755)
			require.NoError(t, err)

			// remove SageOx sections
			err = removeSageoxSections(tmpFile)
			require.NoError(t, err, "removeSageoxSections() error")

			// read output
			output, err := os.ReadFile(tmpFile)
			require.NoError(t, err)
			outputStr := string(output)

			// check wanted content is present
			for _, want := range tt.wantContain {
				assert.Contains(t, outputStr, want, "output missing expected content: %q\noutput:\n%s", want, outputStr)
			}

			// check unwanted content is removed
			for _, unwant := range tt.wantNotIn {
				assert.NotContains(t, outputStr, unwant, "output contains unwanted content: %q\noutput:\n%s", unwant, outputStr)
			}

			// Verify the file is still executable where the platform models
			// an executable bit. Windows has none — os.Stat reports 0666 for
			// every regular file — so the assertion would be checking a
			// property that cannot exist rather than a behavior.
			if runtime.GOOS != "windows" {
				info, err := os.Stat(tmpFile)
				require.NoError(t, err)
				assert.NotEqual(t, os.FileMode(0), info.Mode()&0111, "output file is not executable")
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		patterns []string
		want     bool
	}{
		{
			name:     "no match",
			s:        "hello world",
			patterns: []string{"foo", "bar"},
			want:     false,
		},
		{
			name:     "single match",
			s:        "# ox pre-commit hook",
			patterns: []string{"ox pre-commit", "SageOx"},
			want:     true,
		},
		{
			name:     "multiple matches",
			s:        "# ox pre-commit hook with SageOx",
			patterns: []string{"ox pre-commit", "SageOx"},
			want:     true,
		},
		{
			name:     "empty patterns",
			s:        "hello",
			patterns: []string{},
			want:     false,
		},
		{
			name:     "empty string",
			s:        "",
			patterns: []string{"foo"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsAny(tt.s, tt.patterns)
			assert.Equal(t, tt.want, got, "containsAny(%q, %v)", tt.s, tt.patterns)
		})
	}
}
