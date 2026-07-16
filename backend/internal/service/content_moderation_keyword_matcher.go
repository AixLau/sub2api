package service

type contentModerationKeywordMatcher struct {
	nodes           []contentModerationKeywordNode
	edges           []contentModerationKeywordEdge
	rootTransitions [256]int32
	keywords        []string
	normalized      []string
}

type contentModerationKeywordNode struct {
	failure     int32
	bestKeyword int32
	edgeStart   uint32
	edgeCount   uint16
}

type contentModerationKeywordEdge struct {
	target int32
	label  byte
}

type contentModerationKeywordBuildEdge struct {
	target      int32
	nextSibling int32
	label       byte
}

func newContentModerationKeywordMatcher(keywords []string) *contentModerationKeywordMatcher {
	if len(keywords) == 0 {
		return nil
	}

	buildNodes := []contentModerationKeywordNode{newContentModerationKeywordNode()}
	buildEdges := make([]contentModerationKeywordBuildEdge, 0)
	originalKeywords := append([]string(nil), keywords...)
	normalizedKeywords := make([]string, len(keywords))

	for keywordIndex, keyword := range keywords {
		normalizedKeyword := normalizeKeywordComparable(keyword)
		normalizedKeywords[keywordIndex] = normalizedKeyword
		if normalizedKeyword == "" {
			continue
		}
		patterns := []string{normalizedKeyword}
		if shouldUseCompactKeywordMatch(normalizedKeyword) {
			compactKeyword := compactKeywordComparable(normalizedKeyword)
			if compactKeyword != "" && compactKeyword != normalizedKeyword {
				patterns = append(patterns, compactKeyword)
			}
		}
		for _, pattern := range patterns {
			contentModerationKeywordBuildPattern(&buildNodes, &buildEdges, pattern, int32(keywordIndex))
		}
	}

	if len(buildNodes) == 1 {
		return nil
	}

	queue := make([]int32, 0, len(buildNodes)-1)
	var rootTransitions [256]int32
	for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[0]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
		edge := buildEdges[edgeIndex]
		rootTransitions[edge.label] = edge.target
		queue = append(queue, edge.target)
	}

	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		state := queue[queueIndex]
		for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[state]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
			edge := buildEdges[edgeIndex]
			failure := buildNodes[state].failure
			fallback := contentModerationKeywordBuildTransition(buildNodes, buildEdges, failure, edge.label)
			for fallback < 0 && failure != 0 {
				failure = buildNodes[failure].failure
				fallback = contentModerationKeywordBuildTransition(buildNodes, buildEdges, failure, edge.label)
			}
			if fallback >= 0 {
				buildNodes[edge.target].failure = fallback
			}
			buildNodes[edge.target].bestKeyword = minKeywordIndex(
				buildNodes[edge.target].bestKeyword,
				buildNodes[buildNodes[edge.target].failure].bestKeyword,
			)
			queue = append(queue, edge.target)
		}
	}

	edges := make([]contentModerationKeywordEdge, 0, len(buildEdges))
	var outgoing [256]contentModerationKeywordEdge
	for nodeIndex := range buildNodes {
		count := 0
		for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[nodeIndex]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
			edge := buildEdges[edgeIndex]
			outgoing[count] = contentModerationKeywordEdge{target: edge.target, label: edge.label}
			count++
		}
		for index := 1; index < count; index++ {
			current := outgoing[index]
			insertAt := index
			for insertAt > 0 && current.label < outgoing[insertAt-1].label {
				outgoing[insertAt] = outgoing[insertAt-1]
				insertAt--
			}
			outgoing[insertAt] = current
		}
		buildNodes[nodeIndex].edgeStart = uint32(len(edges))
		buildNodes[nodeIndex].edgeCount = uint16(count)
		edges = append(edges, outgoing[:count]...)
	}

	return &contentModerationKeywordMatcher{
		nodes:           buildNodes,
		edges:           edges,
		rootTransitions: rootTransitions,
		keywords:        originalKeywords,
		normalized:      normalizedKeywords,
	}
}

func contentModerationKeywordBuildPattern(
	nodes *[]contentModerationKeywordNode,
	edges *[]contentModerationKeywordBuildEdge,
	pattern string,
	keywordIndex int32,
) {
	state := int32(0)
	for _, label := range []byte(pattern) {
		next := contentModerationKeywordBuildTransition(*nodes, *edges, state, label)
		if next < 0 {
			next = int32(len(*nodes))
			*nodes = append(*nodes, newContentModerationKeywordNode())
			*edges = append(*edges, contentModerationKeywordBuildEdge{
				target:      next,
				nextSibling: contentModerationKeywordBuildFirstEdge((*nodes)[state]),
				label:       label,
			})
			(*nodes)[state].edgeStart = uint32(len(*edges))
		}
		state = next
	}
	if current := (*nodes)[state].bestKeyword; current < 0 || keywordIndex < current {
		(*nodes)[state].bestKeyword = keywordIndex
	}
}

func newContentModerationKeywordNode() contentModerationKeywordNode {
	return contentModerationKeywordNode{bestKeyword: -1}
}

func contentModerationKeywordBuildFirstEdge(node contentModerationKeywordNode) int32 {
	if node.edgeStart == 0 {
		return -1
	}
	return int32(node.edgeStart - 1)
}

func contentModerationKeywordBuildTransition(
	nodes []contentModerationKeywordNode,
	edges []contentModerationKeywordBuildEdge,
	state int32,
	label byte,
) int32 {
	if state < 0 || int(state) >= len(nodes) {
		return -1
	}
	for edgeIndex := contentModerationKeywordBuildFirstEdge(nodes[state]); edgeIndex >= 0; edgeIndex = edges[edgeIndex].nextSibling {
		if edges[edgeIndex].label == label {
			return edges[edgeIndex].target
		}
	}
	return -1
}

func minKeywordIndex(left, right int32) int32 {
	if left < 0 {
		return right
	}
	if right < 0 || left < right {
		return left
	}
	return right
}

func (m *contentModerationKeywordMatcher) Match(text string) (string, bool) {
	if m == nil || text == "" || len(m.nodes) == 0 || len(m.keywords) == 0 {
		return "", false
	}
	normalizedText := normalizeKeywordComparable(text)
	if normalizedText == "" {
		return "", false
	}
	compactText := compactKeywordComparable(normalizedText)
	if !m.hasCandidate(normalizedText) && (compactText == normalizedText || !m.hasCandidate(compactText)) {
		return "", false
	}

	// The automaton keeps the common no-match path linear in the input size. A
	// candidate is confirmed in configured order because boundary checks can
	// invalidate an earlier overlapping automaton output.
	for index, normalizedKeyword := range m.normalized {
		if normalizedKeyword == "" {
			continue
		}
		if _, _, hit := findKeywordComparableSpanWithBoundary(normalizedText, normalizedKeyword); hit {
			return m.keywords[index], true
		}
		if shouldUseCompactKeywordMatch(normalizedKeyword) {
			compactKeyword := compactKeywordComparable(normalizedKeyword)
			if compactKeyword != "" {
				if _, _, hit := findCompactKeywordComparableSpanWithBoundary(normalizedText, compactText, compactKeyword); hit {
					return m.keywords[index], true
				}
			}
		}
	}
	return "", false
}

func (m *contentModerationKeywordMatcher) hasCandidate(text string) bool {
	state := int32(0)
	for index := 0; index < len(text); index++ {
		label := text[index]
		for {
			next := m.next(state, label)
			if next != 0 {
				state = next
				break
			}
			if state == 0 {
				break
			}
			state = m.nodes[state].failure
		}
		if m.nodes[state].bestKeyword >= 0 {
			return true
		}
	}
	return false
}

func (m *contentModerationKeywordMatcher) next(state int32, label byte) int32 {
	if state == 0 {
		return m.rootTransitions[label]
	}
	if state < 0 || int(state) >= len(m.nodes) {
		return 0
	}
	node := m.nodes[state]
	left := int(node.edgeStart)
	right := left + int(node.edgeCount)
	for left < right {
		middle := left + (right-left)/2
		edge := m.edges[middle]
		if edge.label < label {
			left = middle + 1
			continue
		}
		right = middle
	}
	end := int(node.edgeStart) + int(node.edgeCount)
	if left < end && m.edges[left].label == label {
		return m.edges[left].target
	}
	return 0
}
