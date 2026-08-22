package git

import (
	"reflect"
	"testing"
)

func TestParseGitStatusMatchesUngitShape(t *testing.T) {
	got := ParseGitStatus("## main\x00?? new.txt\x00 M changed.txt\x00R  renamed.txt\x00old.txt\x00")
	if got.Branch != "main" {
		t.Fatalf("branch=%q", got.Branch)
	}
	if len(got.Files) != 3 {
		t.Fatalf("files=%#v", got.Files)
	}
	if !got.Files["new.txt"].IsNew || got.Files["new.txt"].Staged {
		t.Fatalf("new=%#v", got.Files["new.txt"])
	}
	if got.Files["renamed.txt"].OldFileName != "old.txt" || got.Files["renamed.txt"].DisplayName != "old.txt → renamed.txt" {
		t.Fatalf("rename=%#v", got.Files["renamed.txt"])
	}
}

func TestParseGitStatusNumstatRename(t *testing.T) {
	got := ParseGitStatusNumstat("1\t2\tfile.txt\x003\t4\t\x00old.txt\x00new.txt\x00")
	want := map[string]NumStat{"file.txt": {Additions: "1", Deletions: "2"}, "new.txt": {Additions: "3", Deletions: "4"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func TestParseGitLogRepresentative(t *testing.T) {
	input := "commit 5867e2766b0a0f81ad59ce9e9895d9b1a3523aa4 37d1154434b70854ed243967e0d7e37aa3564551 (HEAD -> refs/heads/main)\n" +
		"Author:     Test ungit <test@example.com>\n" +
		"AuthorDate: Fri Jan 4 14:54:06 2019 +0100\n" +
		"Commit:     Test ungit <test@example.com>\n" +
		"CommitDate: Fri Jan 4 14:54:06 2019 +0100\n\n" +
		"    parser fixture\n\n" +
		"1\t1\tsource/git-parser.js\x00175\t0\ttest/spec.git-parser.js\x00\x00commit 37d1154434b70854ed243967e0d7e37aa3564551\n" +
		"Author:     Test ungit <test@example.com>\n" +
		"Commit:     Test ungit <test@example.com>\n"
	got, head := ParseGitLog(input)
	if !head || len(got) != 2 {
		t.Fatalf("head=%v commits=%#v", head, got)
	}
	if got[0].Message != "parser fixture" || got[0].Additions != 176 || got[0].Deletions != 1 {
		t.Fatalf("first=%#v", got[0])
	}
	if len(got[0].FileLineDiffs) != 2 || got[0].FileLineDiffs[1].Additions != 175 {
		t.Fatalf("diffs=%#v", got[0].FileLineDiffs)
	}
	if got[0].AuthorName != "Test ungit" || got[0].AuthorEmail != "test@example.com" {
		t.Fatalf("author=%#v", got[0])
	}
}

func TestParsePatchDiffResultMatchesNodeUngitGolden(t *testing.T) {
	input := "diff --git a/a.txt b/a.txt\n" +
		"index 4cb29ea..7c4ec56 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1,3 +1,3 @@\n" +
		" one\n" +
		"-two\n" +
		"-three\n" +
		"+TWO\n" +
		"+THREE\n"
	want := "diff --git a/a.txt b/a.txt\n" +
		"index 4cb29ea..7c4ec56 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1,3 +1,3 @@\n" +
		" one\n" +
		"-two\n" +
		" three\n" +
		"+TWO"
	got := ParsePatchDiffResult([]bool{true, false, true, false}, input)
	if got != want {
		t.Fatalf("patch mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseGitLogDoubleNULCommitBoundary(t *testing.T) {
	// `git log -z --numstat` can emit two NUL separators after a commit with
	// no numstat payload, for example an `ours` merge. Upstream Ungit accepts
	// one-or-more NULs before the next `commit` record.
	input := "commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb cccccccccccccccccccccccccccccccccccccccc (HEAD -> refs/heads/main)\n" +
		"Merge: bbbbbbb ccccccc\n" +
		"Author:     Test <test@example.com>\n" +
		"AuthorDate: Sat Aug 22 12:20:15 2026 +0000\n" +
		"Commit:     Test <test@example.com>\n" +
		"CommitDate: Sat Aug 22 12:20:15 2026 +0000\n\n" +
		"    no-diff merge\n" +
		"\x00\x00commit bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb dddddddddddddddddddddddddddddddddddddddd\n" +
		"Author:     Test <test@example.com>\n" +
		"AuthorDate: Sat Aug 22 12:19:15 2026 +0000\n" +
		"Commit:     Test <test@example.com>\n" +
		"CommitDate: Sat Aug 22 12:19:15 2026 +0000\n\n" +
		"    parent commit\n\n" +
		"1\t0\tfile.txt\x00"

	got, head := ParseGitLog(input)
	if !head {
		t.Fatal("expected HEAD to be detected")
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2: %#v", len(got), got)
	}
	if got[0].SHA1 != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || got[0].Message != "no-diff merge" {
		t.Fatalf("first commit=%#v", got[0])
	}
	if got[1].SHA1 != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || got[1].Message != "parent commit" {
		t.Fatalf("second commit=%#v", got[1])
	}
	if len(got[0].Parents) != 2 || got[0].Parents[0] != got[1].SHA1 {
		t.Fatalf("merge parents=%#v", got[0].Parents)
	}
}
