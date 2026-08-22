package git

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type FileStatus struct {
	FileName    string `json:"fileName"`
	OldFileName string `json:"oldFileName"`
	DisplayName string `json:"displayName"`
	Staged      bool   `json:"staged"`
	Removed     bool   `json:"removed"`
	IsNew       bool   `json:"isNew"`
	Conflict    bool   `json:"conflict"`
	Renamed     bool   `json:"renamed"`
	Type        string `json:"type"`
	Additions   any    `json:"additions"`
	Deletions   any    `json:"deletions"`
}

type Status struct {
	Branch        string                `json:"branch"`
	Files         map[string]FileStatus `json:"files"`
	InRebase      bool                  `json:"inRebase"`
	InMerge       bool                  `json:"inMerge"`
	InCherry      bool                  `json:"inCherry"`
	CommitMessage string                `json:"commitMessage,omitempty"`
	InConflict    bool                  `json:"inConflict"`
}

type NumStat struct {
	Additions string `json:"additions"`
	Deletions string `json:"deletions"`
}

type FileLineDiff struct {
	Additions   any    `json:"additions"`
	Deletions   any    `json:"deletions"`
	FileName    string `json:"fileName"`
	OldFileName string `json:"oldFileName"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
}

type Commit struct {
	Refs              []string       `json:"refs"`
	FileLineDiffs     []FileLineDiff `json:"fileLineDiffs"`
	Additions         int            `json:"additions"`
	Deletions         int            `json:"deletions"`
	SHA1              string         `json:"sha1"`
	Parents           []string       `json:"parents"`
	IsHead            bool           `json:"isHead"`
	AuthorName        string         `json:"authorName,omitempty"`
	AuthorEmail       string         `json:"authorEmail,omitempty"`
	CommitterName     string         `json:"committerName,omitempty"`
	CommitterEmail    string         `json:"committerEmail,omitempty"`
	AuthorDate        string         `json:"authorDate,omitempty"`
	CommitDate        string         `json:"commitDate,omitempty"`
	ReflogID          string         `json:"reflogId,omitempty"`
	ReflogName        string         `json:"reflogName,omitempty"`
	ReflogAuthorName  string         `json:"reflogAuthorName,omitempty"`
	ReflogAuthorEmail string         `json:"reflogAuthorEmail,omitempty"`
	SignatureDate     string         `json:"signatureDate,omitempty"`
	SignatureMade     string         `json:"signatureMade,omitempty"`
	Message           string         `json:"message"`
}

type Branch struct {
	Name    string `json:"name"`
	Current bool   `json:"current,omitempty"`
}

type Remote struct {
	Name     string `json:"name"`
	FetchURL string `json:"fetchUrl,omitempty"`
	PushURL  string `json:"pushUrl,omitempty"`
	URL      string `json:"url,omitempty"`
}

func fileType(name string) string {
	switch strings.ToUpper(filepath.Ext(name)) {
	case ".PNG", ".JPG", ".BMP", ".GIF", ".JPEG":
		return "image"
	default:
		return "text"
	}
}

func ParseGitStatus(text string) Status {
	parts := strings.Split(text, "\x00")
	branch := ""
	if len(parts) > 0 {
		fields := strings.Split(parts[0], " ")
		if len(fields) > 0 {
			branch = fields[len(fields)-1]
		}
	}
	files := map[string]FileStatus{}
	for i := 1; i < len(parts); i++ {
		line := parts[i]
		if line == "" || len(line) < 3 {
			continue
		}
		status := line[:2]
		newName := strings.TrimSpace(line[3:])
		oldName := newName
		display := newName
		if status[0] == 'R' && i+1 < len(parts) {
			i++
			oldName = parts[i]
			display = oldName + " → " + newName
		}
		files[newName] = FileStatus{
			FileName: newName, OldFileName: oldName, DisplayName: display,
			Staged:   status[0] == 'A' || status[0] == 'M',
			Removed:  status[0] == 'D' || status[1] == 'D',
			IsNew:    (status[0] == '?' || status[0] == 'A') && status[1] != 'D',
			Conflict: (status[0] == 'A' && status[1] == 'A') || status[0] == 'U' || status[1] == 'U',
			Renamed:  status[0] == 'R', Type: fileType(newName),
		}
	}
	return Status{Branch: branch, Files: files}
}

func ParseGitStatusNumstat(text string) map[string]NumStat {
	result := map[string]NumStat{}
	for _, e := range parseNumStatEntries(text) {
		result[e.FileName] = NumStat{Additions: e.Additions, Deletions: e.Deletions}
	}
	return result
}

type numStatEntry struct {
	Additions, Deletions  string
	FileName, OldFileName string
}

func parseNumStatEntries(s string) []numStatEntry {
	var out []numStatEntry
	for pos := 0; pos < len(s); {
		t1 := strings.IndexByte(s[pos:], '\t')
		if t1 < 0 {
			break
		}
		t1 += pos
		t2rel := strings.IndexByte(s[t1+1:], '\t')
		if t2rel < 0 {
			break
		}
		t2 := t1 + 1 + t2rel
		add, del := s[pos:t1], s[t1+1:t2]
		pos = t2 + 1
		if pos >= len(s) {
			break
		}
		if s[pos] == 0 {
			pos++
			z1 := strings.IndexByte(s[pos:], 0)
			if z1 < 0 {
				break
			}
			z1 += pos
			oldName := s[pos:z1]
			pos = z1 + 1
			z2 := strings.IndexByte(s[pos:], 0)
			if z2 < 0 {
				break
			}
			z2 += pos
			newName := s[pos:z2]
			pos = z2 + 1
			out = append(out, numStatEntry{add, del, newName, oldName})
		} else {
			z := strings.IndexByte(s[pos:], 0)
			if z < 0 {
				break
			}
			z += pos
			name := s[pos:z]
			pos = z + 1
			out = append(out, numStatEntry{add, del, name, name})
		}
	}
	return out
}

var authorRE = regexp.MustCompile(`([^<]+)<([^>]+)>`)
var reflogIDRE = regexp.MustCompile(`\{(.*?)\}`)

func ParseGitLog(data string) ([]Commit, bool) {
	if strings.TrimSpace(data) == "" {
		return []Commit{}, false
	}
	rows := strings.Split(data, "\n")
	commits := make([]Commit, 0)
	var cur *Commit
	state := "commit"
	isHeadExist := false

	parseCommit := func(row string) {
		row = strings.TrimLeft(row, "\x00")
		if strings.TrimSpace(row) == "" || !strings.HasPrefix(row, "commit ") {
			return
		}
		c := Commit{Refs: []string{}, FileLineDiffs: []FileLineDiff{}, Parents: []string{}}
		refStart := strings.Index(row, "(")
		prefix := row
		if refStart >= 0 {
			prefix = row[:refStart]
		}
		shaFields := strings.Fields(prefix)
		if len(shaFields) > 1 {
			c.SHA1 = shaFields[1]
			if len(shaFields) > 2 {
				c.Parents = append(c.Parents, shaFields[2:]...)
			}
		}
		if refStart > 0 && strings.HasSuffix(row, ")") {
			refs := row[refStart+1 : len(row)-1]
			refs = strings.ReplaceAll(refs, " -> ", ", ")
			if refs != "" {
				c.Refs = strings.Split(refs, ", ")
			}
		}
		for _, ref := range c.Refs {
			if strings.TrimSpace(ref) == "HEAD" {
				c.IsHead = true
				isHeadExist = true
			}
		}
		commits = append(commits, c)
		cur = &commits[len(commits)-1]
		state = "header"
	}

	for i := 0; i < len(rows); i++ {
		row := rows[i]
		if state == "commit" {
			parseCommit(row)
			continue
		}
		if cur == nil {
			continue
		}
		if state == "header" {
			if strings.TrimSpace(row) == "" {
				state = "message"
				continue
			}
			parseLogHeader(cur, row)
			continue
		}
		if state == "message" {
			trimmed := strings.TrimSpace(row)
			if cur.Message != "" {
				cur.Message += "\n"
			}
			cur.Message += trimmed
			next := ""
			if i+1 < len(rows) {
				next = rows[i+1]
			}
			if looksLikeNumStat(next) {
				state = "files"
			}
			if startsNULCommit(next) {
				state = "commit"
			}
			continue
		}
		if state == "files" {
			if strings.HasPrefix(row, "\x00") {
				row = row[1:]
			}
			entries, consumed := parseNumStatPrefix(row)
			for _, e := range entries {
				display := e.FileName
				if e.OldFileName != e.FileName {
					display = e.OldFileName + " → " + e.FileName
				}
				fd := FileLineDiff{Additions: e.Additions, Deletions: e.Deletions, FileName: e.FileName, OldFileName: e.OldFileName, DisplayName: display, Type: fileType(e.FileName)}
				if n, err := strconv.Atoi(e.Additions); err == nil {
					fd.Additions = n
					cur.Additions += n
				}
				if n, err := strconv.Atoi(e.Deletions); err == nil {
					fd.Deletions = n
					cur.Deletions += n
				}
				cur.FileLineDiffs = append(cur.FileLineDiffs, fd)
			}
			rest := ""
			if consumed < len(row) {
				rest = row[consumed:]
			}
			state = "commit"
			if strings.TrimLeft(rest, "\x00") != "" {
				parseCommit(rest)
			}
		}
	}
	for i := range commits {
		commits[i].Message = strings.TrimSpace(commits[i].Message)
	}
	return commits, isHeadExist
}

func startsNULCommit(s string) bool {
	// Match upstream Ungit's /^\u0000+commit/ boundary check exactly.
	// Git can emit two NUL separators when a commit (notably some merges)
	// has no numstat payload. Treat any positive NUL run followed by
	// "commit" as the next record, otherwise the following commit is
	// accidentally swallowed into the current commit message.
	if len(s) == 0 || s[0] != 0 {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(s, "\x00"), "commit")
}

func looksLikeNumStat(s string) bool {
	if strings.HasPrefix(s, "\x00commit") {
		return false
	}
	s = strings.TrimLeft(s, "\x00")
	parts := strings.SplitN(s, "\t", 3)
	if len(parts) < 3 {
		return false
	}
	valid := func(v string) bool {
		if v == "-" {
			return true
		}
		_, err := strconv.Atoi(v)
		return err == nil
	}
	return valid(parts[0]) && valid(parts[1])
}

func parseNumStatPrefix(s string) ([]numStatEntry, int) {
	entries := []numStatEntry{}
	pos := 0
	for pos < len(s) {
		if strings.HasPrefix(s[pos:], "\x00commit") {
			break
		}
		start := pos
		t1rel := strings.IndexByte(s[pos:], '\t')
		if t1rel < 0 {
			break
		}
		t1 := pos + t1rel
		t2rel := strings.IndexByte(s[t1+1:], '\t')
		if t2rel < 0 {
			break
		}
		t2 := t1 + 1 + t2rel
		add, del := s[pos:t1], s[t1+1:t2]
		if !looksLikeNumStat(add + "\t" + del + "\tx") {
			pos = start
			break
		}
		pos = t2 + 1
		if pos >= len(s) {
			break
		}
		if s[pos] == 0 {
			pos++
			z1rel := strings.IndexByte(s[pos:], 0)
			if z1rel < 0 {
				pos = start
				break
			}
			z1 := pos + z1rel
			oldName := s[pos:z1]
			pos = z1 + 1
			z2rel := strings.IndexByte(s[pos:], 0)
			if z2rel < 0 {
				pos = start
				break
			}
			z2 := pos + z2rel
			newName := s[pos:z2]
			pos = z2 + 1
			entries = append(entries, numStatEntry{add, del, newName, oldName})
		} else {
			zrel := strings.IndexByte(s[pos:], 0)
			if zrel < 0 {
				pos = start
				break
			}
			z := pos + zrel
			name := s[pos:z]
			pos = z + 1
			entries = append(entries, numStatEntry{add, del, name, name})
		}
	}
	return entries, pos
}

func parseLogHeader(c *Commit, row string) {
	for _, item := range []struct {
		key string
		fn  func(string)
	}{
		{"Author", func(v string) { c.AuthorName, c.AuthorEmail = parseAuthor(v) }},
		{"Commit", func(v string) { c.CommitterName, c.CommitterEmail = parseAuthor(v) }},
		{"AuthorDate", func(v string) { c.AuthorDate = v }},
		{"CommitDate", func(v string) { c.CommitDate = v }},
		{"Reflog", func(v string) {
			if m := reflogIDRE.FindStringSubmatch(v); len(m) > 1 {
				c.ReflogID = m[1]
			}
			if p := strings.IndexByte(v, ' '); p >= 0 {
				c.ReflogName = strings.TrimPrefix(v[:p], "refs/")
			}
			if a := strings.IndexByte(v, '('); a >= 0 && strings.HasSuffix(v, ")") {
				c.ReflogAuthorName, c.ReflogAuthorEmail = parseAuthor(v[a+1 : len(v)-1])
			}
		}},
		{"gpg", func(v string) {
			if strings.HasPrefix(v, "Signature made") {
				c.SignatureDate = strings.TrimSpace(strings.TrimPrefix(v, "Signature made"))
			}
			if p := strings.Index(v, "Good signature from"); p >= 0 {
				c.SignatureMade = strings.TrimSpace(strings.ReplaceAll(v[p+len("Good signature from"):], "[ultimate]", ""))
			}
			if strings.Contains(v, "Can't check signature") {
				c.SignatureDate = ""
			}
		}},
	} {
		prefix := item.key + ": "
		if strings.HasPrefix(row, prefix) {
			item.fn(strings.TrimSpace(strings.TrimPrefix(row, prefix)))
			return
		}
	}
}

func parseAuthor(v string) (string, string) {
	m := authorRE.FindStringSubmatch(v)
	if len(m) > 2 {
		return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	}
	return v, ""
}

func ParseGitConfig(text string) map[string]string {
	out := map[string]string{}
	for _, row := range strings.Split(text, "\n") {
		parts := strings.Split(row, "=")
		// JavaScript stores undefined for rows without '=', and JSON.stringify omits it.
		if len(parts) < 2 {
			continue
		}
		out[parts[0]] = parts[1]
	}
	return out
}

func ParseGitBranches(text string) []Branch {
	out := []Branch{}
	for _, row := range strings.Split(text, "\n") {
		if strings.TrimSpace(row) == "" {
			continue
		}
		b := Branch{Name: row[2:]}
		if row[0] == '*' {
			b.Current = true
		}
		out = append(out, b)
	}
	return out
}

func ParseGitTags(text string) []string {
	out := []string{}
	for _, v := range strings.Split(text, "\n") {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func ParseGitRemotes(text string) []Remote {
	order := []string{}
	by := map[string]*Remote{}
	for _, row := range strings.Split(text, "\n") {
		if strings.TrimSpace(row) == "" {
			continue
		}
		parts := strings.Split(row, "\t")
		name := parts[0]
		if by[name] == nil {
			by[name] = &Remote{Name: name}
			order = append(order, name)
		}
		if len(parts) > 1 {
			u := parts[1]
			switch {
			case strings.HasSuffix(u, " (fetch)"):
				by[name].FetchURL = strings.TrimSuffix(u, " (fetch)")
			case strings.HasSuffix(u, " (push)"):
				by[name].PushURL = strings.TrimSuffix(u, " (push)")
			default:
				by[name].URL = u
			}
		}
	}
	out := make([]Remote, 0, len(order))
	for _, n := range order {
		out = append(out, *by[n])
	}
	return out
}

func ParseGitSubmodule(text string) []map[string]string {
	if text == "" {
		return []map[string]string{}
	}
	var out []map[string]string
	var cur map[string]string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[submodule") {
			start := strings.Index(line, "\"")
			end := strings.LastIndex(line, "\"")
			name := ""
			if start >= 0 && end > start {
				name = line[start+1 : end]
			}
			cur = map[string]string{"name": name}
			out = append(out, cur)
			continue
		}
		if cur == nil {
			continue
		}
		parts := strings.Split(line, "=")
		key := strings.TrimSpace(parts[0])
		value := ""
		if len(parts) > 1 {
			value = strings.TrimSpace(strings.Join(parts[1:], "="))
		}
		if key == "path" {
			value = filepath.Clean(value)
		}
		if key == "url" {
			cur["rawUrl"] = value
			url := value
			if !strings.HasPrefix(url, "http") {
				if strings.HasPrefix(url, "git:") {
					url = "http" + url[strings.Index(url, ":"):]
				} else if at := strings.Index(url, "@"); at >= 0 {
					url = "http://" + strings.Replace(url[at+1:], ":", "/", 1)
				}
			}
			value = url
		}
		cur[key] = value
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	return out
}

var patchHeaderRE = regexp.MustCompile(`@@ -[0-9]+,[0-9]+ \+[0-9]+,[0-9]+ @@`)

// ParsePatchDiffResult reproduces Ungit-compatible partial-staging transformation. Each
// boolean corresponds, in order, to a +/- line in the diff. Selected changes
// are retained while unselected additions are removed and unselected deletions
// become context lines. Hunk headers are adjusted to keep the resulting patch
// valid for git apply --cached.
func ParsePatchDiffResult(patchLineList []bool, text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	result := []string{}
	ignoredDiffCountTotal := 0
	ignoredDiffCountCurrent := 0
	lastHeaderIndex := -1
	n := 0
	selectedLines := 0
	selection := append([]bool(nil), patchLineList...)

	for n < len(lines) && !patchHeaderRE.MatchString(lines[n]) {
		result = append(result, lines[n])
		n++
	}
	if n == len(lines) {
		return ""
	}

	updateHeader := func() {
		if lastHeaderIndex < 0 || lastHeaderIndex >= len(result) {
			return
		}
		parts := strings.Split(result[lastHeaderIndex], " ")
		if len(parts) < 3 {
			return
		}
		start := strings.Split(parts[1], ",")
		end := strings.Split(parts[2], ",")
		if len(start) < 2 || len(end) < 2 {
			return
		}
		startLeft, err := strconv.Atoi(strings.TrimPrefix(start[0], "-"))
		if err != nil {
			return
		}
		endLeft, err := strconv.Atoi(strings.TrimPrefix(end[0], "+"))
		if err != nil {
			return
		}
		parts[1] = fmt.Sprintf("-%d,%s", startLeft-ignoredDiffCountTotal, start[1])
		parts[2] = fmt.Sprintf("+%d,%d", endLeft-ignoredDiffCountTotal, mustAtoi(end[1])-ignoredDiffCountCurrent)

		allSpace := true
		for i := lastHeaderIndex + 1; i < len(result); i++ {
			if result[i] == "" || result[i][0] != ' ' {
				allSpace = false
				break
			}
		}
		if allSpace {
			result = result[:lastHeaderIndex]
		} else {
			result[lastHeaderIndex] = strings.Join(parts, " ")
		}
	}

	for n < len(lines) {
		line := lines[n]
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			selected := false
			if len(selection) > 0 {
				selected = selection[0]
				selection = selection[1:]
			}
			if selected {
				selectedLines++
				result = append(result, line)
			} else if strings.HasPrefix(line, "+") {
				ignoredDiffCountCurrent++
			} else {
				ignoredDiffCountCurrent--
				result = append(result, " "+line[1:])
			}
		} else {
			if patchHeaderRE.MatchString(line) {
				if lastHeaderIndex > -1 {
					updateHeader()
				}
				ignoredDiffCountTotal += ignoredDiffCountCurrent
				ignoredDiffCountCurrent = 0
				lastHeaderIndex = len(result)
			}
			result = append(result, line)
		}
		n++
	}
	updateHeader()
	if selectedLines == 0 {
		return ""
	}
	return strings.Join(result, "\n")
}

func mustAtoi(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}
