package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type gitRepo struct {
	root string
}

func (g gitRepo) run(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", g.root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return string(out), errors.New(message)
	}
	return string(out), nil
}

func (g gitRepo) resolve(ref string) (string, error) {
	out, err := g.run("rev-parse", "--verify", ref+"^{commit}")
	return strings.TrimSpace(out), err
}

func (g gitRepo) blob(ref, path string) string {
	out, err := g.run("rev-parse", ref+":"+path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (g gitRepo) content(ref, path string) ([]byte, error) {
	out, err := g.run("show", ref+":"+path)
	return []byte(out), err
}

func (g gitRepo) ancestor(older, newer string) bool {
	cmd := exec.Command("git", "-C", g.root, "merge-base", "--is-ancestor", older, newer)
	return cmd.Run() == nil
}

func (g gitRepo) countCommits(from, to string) int {
	out, err := g.run("rev-list", "--count", from+".."+to)
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(out))
	return count
}

func (g gitRepo) changedFiles(from, to string) ([]ChangedFile, error) {
	out, err := g.run("diff", "--raw", "--abbrev=40", "-z", "-M", "-C", from, to)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out, "\x00")
	var files []ChangedFile
	for i := 0; i < len(parts); {
		header := parts[i]
		i++
		if header == "" {
			continue
		}
		fields := strings.Fields(header)
		if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") {
			return nil, fmt.Errorf("invalid git raw record %q", header)
		}
		oldBlob := fields[2]
		newBlob := fields[3]
		status := fields[4]
		kind := status[:1]
		if kind == "R" || kind == "C" {
			if i+1 >= len(parts) {
				return nil, fmt.Errorf("invalid git rename record")
			}
			oldPath, newPath := parts[i], parts[i+1]
			i += 2
			files = append(files, ChangedFile{
				Status:  kind,
				OldPath: filepath.ToSlash(oldPath),
				Path:    filepath.ToSlash(newPath),
				OldBlob: nonzeroBlob(oldBlob),
				NewBlob: nonzeroBlob(newBlob),
			})
			continue
		}
		if i >= len(parts) {
			return nil, fmt.Errorf("invalid git change record")
		}
		path := filepath.ToSlash(parts[i])
		i++
		files = append(files, ChangedFile{
			Status:  kind,
			Path:    path,
			OldBlob: nonzeroBlob(oldBlob),
			NewBlob: nonzeroBlob(newBlob),
		})
	}
	for i := range files {
		files[i].Classification = classifyPath(files[i].Path)
	}
	return files, nil
}

func nonzeroBlob(blob string) string {
	if strings.Trim(blob, "0") == "" {
		return ""
	}
	return blob
}

func (g gitRepo) mergeBase(left, right string) string {
	out, err := g.run("merge-base", left, right)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (g gitRepo) simulate(local, upstream string) Simulation {
	result := Simulation{Base: g.mergeBase(local, upstream)}
	out, err := g.run("merge-tree", "--write-tree", local, upstream)
	result.RawOutput = trimBytes(out, 16384)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if err == nil && len(lines) > 0 && len(strings.TrimSpace(lines[0])) == 40 {
		result.Tree = strings.TrimSpace(lines[0])
		result.Clean = true
		return result
	}
	for _, line := range lines {
		if strings.Contains(line, "CONFLICT") {
			result.Conflicts = append(result.Conflicts, strings.TrimSpace(line))
		}
	}
	result.Conflicts = unique(result.Conflicts)
	return result
}

func (g gitRepo) diffSnippet(from, to, path string, limit int) string {
	out, err := g.run("diff", "--unified=3", from, to, "--", path)
	if err != nil {
		return ""
	}
	return trimBytes(out, limit)
}

func (g gitRepo) trackedBlobs(ref string) map[string]string {
	out, err := g.run("ls-tree", "-r", "-z", ref)
	if err != nil {
		return nil
	}
	blobs := make(map[string]string)
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		header, pathValue, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		blobs[filepath.ToSlash(pathValue)] = fields[2]
	}
	return blobs
}

func trimBytes(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	const marker = "\n... [truncated]\n"
	if limit <= len(marker) {
		return value[:limit]
	}
	return value[:limit-len(marker)] + marker
}
