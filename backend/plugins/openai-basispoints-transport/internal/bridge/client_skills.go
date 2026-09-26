package bridge

import (
	"encoding/json"
	"path"
	"strings"
)

// Only the latest client-owned skills block is authoritative. User messages,
// tool results and the gateway filesystem cannot supply executable skill paths.
type clientSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

func readClientSkills(instructions json.RawMessage, input []json.RawMessage) ([]clientSkill, bool) {
	texts := []string{stringValue(instructions)}
	for _, raw := range input {
		item, err := parseObject(raw)
		if err != nil || (stringValue(item["role"]) != "developer" && stringValue(item["role"]) != "system") {
			continue
		}
		if isTextValue(item["content"]) {
			texts = append(texts, stringValue(item["content"]))
			continue
		}
		var parts []object
		if json.Unmarshal(item["content"], &parts) == nil {
			var text strings.Builder
			for _, part := range parts {
				if typ := stringValue(part["type"]); typ == "input_text" || typ == "text" {
					text.WriteString(stringValue(part["text"]))
				}
			}
			texts = append(texts, text.String())
		}
	}
	var block string
	for _, text := range texts {
		for {
			_, rest, found := strings.Cut(text, "<skills_instructions>")
			if !found {
				break
			}
			candidate, tail, closed := strings.Cut(rest, "</skills_instructions>")
			if !closed {
				break
			}
			block, text = candidate, tail
		}
	}
	if block == "" || !strings.Contains(block, "### Available skills") {
		return nil, false
	}
	roots := map[string]string{}
	section := ""
	skills := []clientSkill{}
	seen := map[string]bool{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "##") {
			section = line
			continue
		}
		entry, ok := strings.CutPrefix(line, "- ")
		if !ok {
			continue
		}
		if section == "### Skill roots" {
			alias, root, ok := strings.Cut(entry, " = ")
			if ok {
				roots[strings.Trim(alias, string(rune(96)))] = strings.Trim(root, string(rune(96)))
			}
			continue
		}
		if section != "### Available skills" {
			continue
		}
		name, rest, ok := strings.Cut(entry, ": ")
		loc := strings.LastIndex(rest, " (file: ")
		if !ok || name == "" || loc < 0 || !strings.HasSuffix(rest, ")") {
			return nil, false
		}
		file := rest[loc+len(" (file: ") : len(rest)-1]
		file = strings.ReplaceAll(file, "\\", "/")
		if alias, tail, found := strings.Cut(file, "/"); found && roots[alias] != "" {
			file = strings.TrimRight(strings.ReplaceAll(roots[alias], "\\", "/"), "/") + "/" + tail
		}
		if !absoluteClientPath(file) || strings.ContainsAny(file, "\x00\r\n") || strings.Contains(file, "/../") {
			return nil, false
		}
		file = path.Clean(file)
		if seen[name] {
			return nil, false
		}
		seen[name] = true
		skills = append(skills, clientSkill{ID: name, Name: name, Description: rest[:loc], Path: file})
	}
	if len(skills) == 0 && strings.Contains(block, "(file:") {
		return nil, false
	}
	return skills, true
}

func absoluteClientPath(file string) bool {
	return strings.HasPrefix(file, "/") || (len(file) > 3 && file[1] == ':' && file[2] == '/' && ((file[0] >= 'A' && file[0] <= 'Z') || (file[0] >= 'a' && file[0] <= 'z')))
}
