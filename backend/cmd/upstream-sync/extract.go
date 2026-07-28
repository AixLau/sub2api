package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type indexer struct {
	repo   gitRepo
	cache  string
	memo   map[string]SourceIndex
	parsed int
	reused int
}

func newIndexer(repo gitRepo) (*indexer, error) {
	gitDir, err := repo.run("rev-parse", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repo.root, gitDir)
	}
	cache := filepath.Join(gitDir, "upstream-sync", "blob-index")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return nil, err
	}
	return &indexer{repo: repo, cache: cache, memo: make(map[string]SourceIndex)}, nil
}

func (i *indexer) at(ref, path string) (SourceIndex, error) {
	key := ref + "\x00" + path
	if index, ok := i.memo[key]; ok {
		return index, nil
	}
	blob := i.repo.blob(ref, path)
	return i.atBlob(ref, path, blob)
}

func (i *indexer) atBlob(ref, path, blob string) (SourceIndex, error) {
	key := ref + "\x00" + path
	if index, ok := i.memo[key]; ok {
		return index, nil
	}
	if blob == "" {
		index := SourceIndex{Analyzer: analyzerVersion, Path: path}
		i.memo[key] = index
		return index, nil
	}
	cachePath := filepath.Join(i.cache, blob+".json")
	if data, err := os.ReadFile(cachePath); err == nil {
		var index SourceIndex
		if json.Unmarshal(data, &index) == nil && index.Analyzer == analyzerVersion {
			index.Path = path
			i.reused++
			i.memo[key] = index
			return index, nil
		}
	}
	content, err := i.repo.content(ref, path)
	if err != nil {
		return SourceIndex{}, err
	}
	index := extractSource(path, blob, content)
	data, _ := json.Marshal(index)
	_ = os.WriteFile(cachePath, data, 0o644)
	i.parsed++
	i.memo[key] = index
	return index, nil
}

func extractSource(path, blob string, content []byte) SourceIndex {
	index := SourceIndex{
		Analyzer:  analyzerVersion,
		Path:      path,
		Blob:      blob,
		Contracts: make(map[string][]string),
	}
	switch {
	case strings.HasSuffix(path, ".go"):
		index.Language = "go"
		extractGo(&index, content)
	case strings.HasSuffix(path, ".vue"):
		index.Language = "vue"
		extractFrontend(&index, content)
	case strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".tsx"), strings.HasSuffix(path, ".js"), strings.HasSuffix(path, ".jsx"):
		index.Language = "typescript"
		extractFrontend(&index, content)
	case strings.HasSuffix(path, ".sql"):
		index.Language = "sql"
		extractSQL(&index, content)
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"), strings.HasSuffix(path, ".json"):
		index.Language = "config"
		extractConfig(&index, content)
	default:
		index.Language = "text"
	}
	for key, values := range index.Contracts {
		index.Contracts[key] = unique(values)
	}
	return index
}

func extractGo(index *SourceIndex, content []byte) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, index.Path, content, parser.ParseComments)
	if err != nil {
		return
	}
	index.Package = file.Name.Name
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
			receiver := ""
			if node.Recv != nil && len(node.Recv.List) > 0 {
				receiver = receiverName(node.Recv.List[0].Type)
			}
			name := node.Name.Name
			display := name
			if receiver != "" {
				display = receiver + "." + name
			}
			start, end := fset.Position(node.Pos()).Line, fset.Position(node.End()).Line
			raw := sourceLines(content, start, end)
			symbol := Symbol{
				Key:       "func:" + display,
				Name:      display,
				Kind:      "function",
				Receiver:  receiver,
				Exported:  ast.IsExported(name),
				StartLine: start,
				EndLine:   end,
				Signature: functionSignature(node),
				Hash:      hashText(raw),
			}
			ast.Inspect(node.Body, func(child ast.Node) bool {
				switch value := child.(type) {
				case *ast.CallExpr:
					if called := expressionName(value.Fun); called != "" {
						symbol.Calls = append(symbol.Calls, called)
					}
				case *ast.SelectorExpr:
					symbol.Refs = append(symbol.Refs, expressionName(value))
				case *ast.BasicLit:
					if value.Kind == token.STRING {
						extractStringContract(index.Contracts, unquote(value.Value))
					}
				}
				return true
			})
			symbol.Calls = unique(symbol.Calls)
			symbol.Refs = unique(symbol.Refs)
			index.Symbols = append(index.Symbols, symbol)
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					start, end := fset.Position(value.Pos()).Line, fset.Position(value.End()).Line
					kind := "type"
					switch value.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					index.Symbols = append(index.Symbols, Symbol{
						Key:       kind + ":" + value.Name.Name,
						Name:      value.Name.Name,
						Kind:      kind,
						Exported:  ast.IsExported(value.Name.Name),
						StartLine: start,
						EndLine:   end,
						Hash:      hashText(sourceLines(content, start, end)),
					})
					ast.Inspect(value.Type, func(child ast.Node) bool {
						if field, ok := child.(*ast.Field); ok && field.Tag != nil {
							extractTagContracts(index.Contracts, unquote(field.Tag.Value))
						}
						return true
					})
				case *ast.ValueSpec:
					kind := strings.ToLower(node.Tok.String())
					for _, name := range value.Names {
						start, end := fset.Position(value.Pos()).Line, fset.Position(value.End()).Line
						index.Symbols = append(index.Symbols, Symbol{
							Key:       kind + ":" + name.Name,
							Name:      name.Name,
							Kind:      kind,
							Exported:  ast.IsExported(name.Name),
							StartLine: start,
							EndLine:   end,
							Hash:      hashText(sourceLines(content, start, end)),
						})
					}
					for _, expr := range value.Values {
						ast.Inspect(expr, func(child ast.Node) bool {
							if literal, ok := child.(*ast.BasicLit); ok && literal.Kind == token.STRING {
								extractStringContract(index.Contracts, unquote(literal.Value))
							}
							return true
						})
					}
				}
			}
		}
	}
	wireRE := regexp.MustCompile(`wire\.(?:NewSet|Bind|Struct|FieldsOf|Value|InterfaceValue)\s*\(`)
	if wireRE.Match(content) {
		index.Contracts["wire"] = append(index.Contracts["wire"], "wire.ProviderSet")
	}
	extractRoutes(index.Contracts, string(content))
	extractEnv(index.Contracts, string(content))
}

func functionSignature(node *ast.FuncDecl) string {
	params, results := 0, 0
	if node.Type.Params != nil {
		params = node.Type.Params.NumFields()
	}
	if node.Type.Results != nil {
		results = node.Type.Results.NumFields()
	}
	return fmt.Sprintf("%s(%d)->%d", node.Name.Name, params, results)
}

func receiverName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	}
	return ""
}

func expressionName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		left := expressionName(value.X)
		if left == "" {
			return value.Sel.Name
		}
		return left + "." + value.Sel.Name
	case *ast.IndexExpr:
		return expressionName(value.X)
	case *ast.IndexListExpr:
		return expressionName(value.X)
	case *ast.ParenExpr:
		return expressionName(value.X)
	}
	return ""
}

func sourceLines(content []byte, start, end int) string {
	lines := bytes.Split(content, []byte("\n"))
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return ""
	}
	return string(bytes.Join(lines[start-1:end], []byte("\n")))
}

func extractTagContracts(contracts map[string][]string, tag string) {
	for _, kind := range []string{"json", "mapstructure", "yaml", "form", "query"} {
		re := regexp.MustCompile(kind + `:"([^",]+)`)
		for _, match := range re.FindAllStringSubmatch(tag, -1) {
			contracts[kind] = append(contracts[kind], match[1])
		}
	}
}

func extractStringContract(contracts map[string][]string, value string) {
	if value == "" || len(value) > 180 {
		return
	}
	switch {
	case strings.HasPrefix(value, "/"):
		contracts["route"] = append(contracts["route"], value)
	case strings.Contains(value, ".") && !strings.ContainsAny(value, " \t\n"):
		contracts["key"] = append(contracts["key"], value)
	case strings.Contains(value, "_") && !strings.ContainsAny(value, " \t\n"):
		contracts["field"] = append(contracts["field"], value)
	}
}

func extractRoutes(contracts map[string][]string, source string) {
	routeRE := regexp.MustCompile(`\.(GET|POST|PUT|PATCH|DELETE|Any|Handle)\s*\(\s*["']([^"']+)["']`)
	for _, match := range routeRE.FindAllStringSubmatch(source, -1) {
		contracts["route"] = append(contracts["route"], strings.ToUpper(match[1])+" "+match[2])
	}
}

func extractEnv(contracts map[string][]string, source string) {
	envRE := regexp.MustCompile(`(?:Getenv|LookupEnv|BindEnv)\s*\(\s*["']([A-Z][A-Z0-9_]+)["']`)
	for _, match := range envRE.FindAllStringSubmatch(source, -1) {
		contracts["env"] = append(contracts["env"], match[1])
	}
}

func extractFrontend(index *SourceIndex, content []byte) {
	source := string(content)
	component := strings.TrimSuffix(filepath.Base(index.Path), filepath.Ext(index.Path))
	index.Symbols = append(index.Symbols, Symbol{
		Key:       "component:" + component,
		Name:      component,
		Kind:      "component",
		Exported:  true,
		StartLine: 1,
		EndLine:   bytes.Count(content, []byte("\n")) + 1,
		Hash:      hashText(source),
	})
	patterns := []struct {
		kind string
		re   *regexp.Regexp
	}{
		{"function", regexp.MustCompile(`(?m)(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*(?:<[^>{}]+>)?\s*\(`)},
		{"function", regexp.MustCompile(`(?m)(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\([^)]*\)\s*=>`)},
		{"composable", regexp.MustCompile(`(?m)(?:export\s+)?(?:function|const)\s+(use[A-Z][A-Za-z0-9_$]*)`)},
		{"store", regexp.MustCompile(`defineStore\s*\(\s*["']([^"']+)["']`)},
		{"declaration", regexp.MustCompile(`(?m)(?:export\s+)?(?:const|let|class|interface|type)\s+([A-Za-z_$][\w$]*)`)},
	}
	for _, pattern := range patterns {
		for _, match := range pattern.re.FindAllStringSubmatchIndex(source, -1) {
			name := source[match[2]:match[3]]
			line := strings.Count(source[:match[0]], "\n") + 1
			index.Symbols = append(index.Symbols, Symbol{
				Key:       pattern.kind + ":" + name,
				Name:      name,
				Kind:      pattern.kind,
				Exported:  true,
				StartLine: line,
				EndLine:   line,
				Hash:      hashText(source[match[0]:match[1]]),
			})
		}
	}
	extractFrontendContracts(index.Contracts, source)
	extractRoutes(index.Contracts, source)
}

func extractFrontendContracts(contracts map[string][]string, source string) {
	for _, match := range regexp.MustCompile(`defineProps\s*<([^>]+)>`).FindAllStringSubmatch(source, -1) {
		for _, field := range regexp.MustCompile(`([A-Za-z_$][\w$]*)\??\s*:`).FindAllStringSubmatch(match[1], -1) {
			contracts["prop"] = append(contracts["prop"], field[1])
		}
	}
	for _, match := range regexp.MustCompile(`defineEmits\s*<([^>]+)>`).FindAllStringSubmatch(source, -1) {
		for _, event := range regexp.MustCompile(`["']([^"']+)["']`).FindAllStringSubmatch(match[1], -1) {
			contracts["emit"] = append(contracts["emit"], event[1])
		}
	}
	for _, match := range regexp.MustCompile(`(?:\$?t|i18n\.t)\s*\(\s*["']([^"']+)["']`).FindAllStringSubmatch(source, -1) {
		contracts["i18n"] = append(contracts["i18n"], match[1])
	}
	for _, match := range regexp.MustCompile(`(?m)\bpath\s*:\s*["']([^"']+)["']`).FindAllStringSubmatch(source, -1) {
		contracts["route"] = append(contracts["route"], match[1])
	}
	for _, match := range regexp.MustCompile("\\b(?:api|http|client)\\.(get|post|put|patch|delete)\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]").FindAllStringSubmatch(source, -1) {
		contracts["api"] = append(contracts["api"], strings.ToUpper(match[1])+" "+match[2])
	}
	for _, match := range regexp.MustCompile(`import\.meta\.env\.([A-Z][A-Z0-9_]+)`).FindAllStringSubmatch(source, -1) {
		contracts["env"] = append(contracts["env"], match[1])
	}
}

func extractSQL(index *SourceIndex, content []byte) {
	source := string(content)
	tableRE := regexp.MustCompile(`(?i)(?:create\s+table|alter\s+table|update|insert\s+into)\s+(?:if\s+not\s+exists\s+)?["]?([a-zA-Z_][\w]*)`)
	for _, match := range tableRE.FindAllStringSubmatch(source, -1) {
		index.Contracts["table"] = append(index.Contracts["table"], strings.ToLower(match[1]))
	}
	columnRE := regexp.MustCompile(`(?im)^\s*["]?([a-zA-Z_][\w]*)["]?\s+(?:varchar|text|integer|bigint|numeric|decimal|boolean|timestamp|jsonb|uuid)\b`)
	for _, match := range columnRE.FindAllStringSubmatch(source, -1) {
		index.Contracts["database"] = append(index.Contracts["database"], strings.ToLower(match[1]))
	}
}

func extractConfig(index *SourceIndex, content []byte) {
	keyRE := regexp.MustCompile(`(?m)^\s*["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?\s*:`)
	for _, match := range keyRE.FindAllStringSubmatch(string(content), -1) {
		index.Contracts["key"] = append(index.Contracts["key"], match[1])
	}
}

func unquote(value string) string {
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return strings.Trim(value, "`\"'")
}

func compareIndexes(oldIndex, newIndex SourceIndex) (changed, added, deleted []string, contracts map[string][]string) {
	oldSymbols := make(map[string]Symbol)
	newSymbols := make(map[string]Symbol)
	for _, symbol := range oldIndex.Symbols {
		oldSymbols[symbol.Key] = symbol
	}
	for _, symbol := range newIndex.Symbols {
		newSymbols[symbol.Key] = symbol
	}
	for key, oldSymbol := range oldSymbols {
		newSymbol, exists := newSymbols[key]
		if !exists {
			deleted = append(deleted, oldSymbol.Name)
			continue
		}
		if oldSymbol.Hash != newSymbol.Hash || oldSymbol.Signature != newSymbol.Signature {
			changed = append(changed, newSymbol.Name)
		}
	}
	for key, symbol := range newSymbols {
		if _, exists := oldSymbols[key]; !exists {
			added = append(added, symbol.Name)
		}
	}
	contracts = make(map[string][]string)
	kinds := make(map[string]struct{})
	for kind := range oldIndex.Contracts {
		kinds[kind] = struct{}{}
	}
	for kind := range newIndex.Contracts {
		kinds[kind] = struct{}{}
	}
	for kind := range kinds {
		values := symmetricDifference(oldIndex.Contracts[kind], newIndex.Contracts[kind])
		if len(values) > 0 {
			contracts[kind] = values
		}
	}
	return unique(changed), unique(added), unique(deleted), contracts
}

func symmetricDifference(left, right []string) []string {
	count := make(map[string]int)
	for _, value := range unique(left) {
		count[value]++
	}
	for _, value := range unique(right) {
		count[value]++
	}
	var out []string
	for value, occurrences := range count {
		if occurrences == 1 {
			out = append(out, value)
		}
	}
	return unique(out)
}

func classifyPath(path string) []string {
	var classes []string
	switch {
	case strings.HasPrefix(path, "backend/ent/") && !strings.HasPrefix(path, "backend/ent/schema/"):
		classes = append(classes, "generated")
	case path == "backend/cmd/server/wire_gen.go":
		classes = append(classes, "generated")
	}
	if strings.HasSuffix(path, "go.sum") || strings.HasSuffix(path, "pnpm-lock.yaml") || strings.HasSuffix(path, "package-lock.json") {
		classes = append(classes, "lock", "dependency")
	}
	if strings.HasSuffix(path, "go.mod") || strings.HasSuffix(path, "package.json") {
		classes = append(classes, "dependency")
	}
	if strings.HasPrefix(path, "docs/") || strings.HasSuffix(path, ".md") || path == "LICENSE" {
		classes = append(classes, "documentation")
	}
	if strings.HasPrefix(path, "frontend/src/i18n/") {
		classes = append(classes, "internationalization")
	}
	if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "__tests__") || strings.HasSuffix(path, ".spec.ts") || strings.HasSuffix(path, ".test.ts") {
		classes = append(classes, "test")
	}
	return unique(classes)
}
