package service

type contentModerationKeywordMatcher struct {
	nodes                 []contentModerationKeywordNode
	edges                 []contentModerationKeywordEdge
	outputs               []contentModerationKeywordOutput
	rootTransitions       [256]int32
	keywords              []string
	hasCompact            bool
	outputGroupCount      int
	matchableKeywordCount int
	minimumKeyword        int32
}

type contentModerationPreparedRule struct {
	rule ContentModerationKeywordRule
}

type contentModerationPreparedRuleSet struct {
	rules    []ContentModerationKeywordRule
	prepared []contentModerationPreparedRule
	matcher  *contentModerationKeywordMatcher
}

func newContentModerationPreparedRuleSet(rules []ContentModerationKeywordRule) *contentModerationPreparedRuleSet {
	rules = normalizeContentModerationKeywordRules(rules)
	set := &contentModerationPreparedRuleSet{
		rules:    rules,
		prepared: make([]contentModerationPreparedRule, len(rules)),
	}
	keywords := make([]string, len(rules))
	for index, rule := range rules {
		set.prepared[index] = contentModerationPreparedRule{
			rule: rule,
		}
		if rule.Enabled {
			keywords[index] = rule.Keyword
		}
	}
	set.matcher = newContentModerationKeywordMatcher(keywords)
	return set
}

func (s *contentModerationPreparedRuleSet) Match(text string) (ContentModerationKeywordRule, bool) {
	if s == nil || s.matcher == nil || text == "" || len(s.prepared) == 0 {
		return ContentModerationKeywordRule{}, false
	}
	normalizedText := normalizeKeywordComparable(text)
	index, ok := s.matcher.firstMatchNormalized(normalizedText)
	if !ok || index < 0 || int(index) >= len(s.prepared) {
		return ContentModerationKeywordRule{}, false
	}
	return s.prepared[index].rule, true
}

func (s *contentModerationPreparedRuleSet) Matches(text string) []ContentModerationKeywordRule {
	return s.MatchesNormalized(normalizeKeywordComparable(text))
}

func (s *contentModerationPreparedRuleSet) MatchesNormalized(normalizedText string) []ContentModerationKeywordRule {
	if s == nil || s.matcher == nil || normalizedText == "" || len(s.prepared) == 0 {
		return nil
	}
	accumulator := contentModerationKeywordMatchAccumulator{
		best:       -1,
		collectAll: true,
	}
	s.matcher.scanNormalized(normalizedText, &accumulator)
	if accumulator.hitCount == 0 {
		return nil
	}
	matches := make([]ContentModerationKeywordRule, 0, accumulator.hitCount)
	if len(accumulator.hitWords) != 0 {
		for keywordIndex := range s.prepared {
			if accumulator.hasKeyword(int32(keywordIndex)) {
				matches = append(matches, s.prepared[keywordIndex].rule)
			}
		}
		return matches
	}
	accumulator.sortInlineHits()
	for index := 0; index < accumulator.hitCount; index++ {
		keywordIndex := accumulator.inlineHits[index]
		if keywordIndex >= 0 && int(keywordIndex) < len(s.prepared) {
			matches = append(matches, s.prepared[keywordIndex].rule)
		}
	}
	return matches
}

type contentModerationKeywordNode struct {
	failure      int32
	outputLink   int32
	terminalHead int32
	patternBytes int32
	directMin    int32
	compactMin   int32
	directGroup  int32
	compactGroup int32
	edgeStart    uint32
	edgeCount    uint16
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

const (
	contentModerationKeywordModeDirect uint8 = 1 << iota
	contentModerationKeywordModeCompact
)

type contentModerationKeywordOutput struct {
	keywordIndex int32
	next         int32
	modes        uint8
}

func newContentModerationKeywordMatcher(keywords []string) *contentModerationKeywordMatcher {
	if len(keywords) == 0 {
		return nil
	}

	buildNodes := []contentModerationKeywordNode{newContentModerationKeywordNode()}
	buildEdges := make([]contentModerationKeywordBuildEdge, 0)
	buildOutputs := make([]contentModerationKeywordOutput, 0, len(keywords))
	originalKeywords := append([]string(nil), keywords...)
	hasCompact := false
	outputGroupCount := 0
	matchableKeywordCount := 0
	minimumKeyword := int32(-1)

	for keywordIndex, keyword := range keywords {
		normalizedKeyword := normalizeKeywordComparable(keyword)
		if normalizedKeyword == "" {
			continue
		}
		matchableKeywordCount++
		if minimumKeyword < 0 || int32(keywordIndex) < minimumKeyword {
			minimumKeyword = int32(keywordIndex)
		}
		compactEnabled := shouldUseCompactKeywordMatch(normalizedKeyword)
		compactKeyword := ""
		directModes := contentModerationKeywordModeDirect
		if compactEnabled {
			hasCompact = true
			compactKeyword = compactKeywordComparable(normalizedKeyword)
			if compactKeyword == normalizedKeyword {
				directModes |= contentModerationKeywordModeCompact
			}
		}
		contentModerationKeywordBuildPattern(
			&buildNodes,
			&buildEdges,
			&buildOutputs,
			&outputGroupCount,
			normalizedKeyword,
			int32(keywordIndex),
			directModes,
		)
		if compactEnabled && compactKeyword != "" && compactKeyword != normalizedKeyword {
			contentModerationKeywordBuildPattern(
				&buildNodes,
				&buildEdges,
				&buildOutputs,
				&outputGroupCount,
				compactKeyword,
				int32(keywordIndex),
				contentModerationKeywordModeCompact,
			)
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
			failureNode := buildNodes[edge.target].failure
			if buildNodes[failureNode].terminalHead >= 0 {
				buildNodes[edge.target].outputLink = failureNode
			} else {
				buildNodes[edge.target].outputLink = buildNodes[failureNode].outputLink
			}
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
		nodes:                 buildNodes,
		edges:                 edges,
		outputs:               buildOutputs,
		rootTransitions:       rootTransitions,
		keywords:              originalKeywords,
		hasCompact:            hasCompact,
		outputGroupCount:      outputGroupCount,
		matchableKeywordCount: matchableKeywordCount,
		minimumKeyword:        minimumKeyword,
	}
}

func contentModerationKeywordBuildPattern(
	nodes *[]contentModerationKeywordNode,
	edges *[]contentModerationKeywordBuildEdge,
	outputs *[]contentModerationKeywordOutput,
	outputGroupCount *int,
	pattern string,
	keywordIndex int32,
	modes uint8,
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
	node := &(*nodes)[state]
	node.patternBytes = int32(len(pattern))
	if modes&contentModerationKeywordModeDirect != 0 {
		if node.directGroup < 0 {
			node.directGroup = int32(*outputGroupCount)
			(*outputGroupCount)++
		}
		if node.directMin < 0 || keywordIndex < node.directMin {
			node.directMin = keywordIndex
		}
	}
	if modes&contentModerationKeywordModeCompact != 0 {
		if node.compactGroup < 0 {
			node.compactGroup = int32(*outputGroupCount)
			(*outputGroupCount)++
		}
		if node.compactMin < 0 || keywordIndex < node.compactMin {
			node.compactMin = keywordIndex
		}
	}
	*outputs = append(*outputs, contentModerationKeywordOutput{
		keywordIndex: keywordIndex,
		next:         node.terminalHead,
		modes:        modes,
	})
	node.terminalHead = int32(len(*outputs) - 1)
}

func newContentModerationKeywordNode() contentModerationKeywordNode {
	return contentModerationKeywordNode{
		outputLink: -1, terminalHead: -1,
		directMin: -1, compactMin: -1, directGroup: -1, compactGroup: -1,
	}
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

func (m *contentModerationKeywordMatcher) Match(text string) (string, bool) {
	if m == nil || text == "" || len(m.nodes) == 0 || len(m.keywords) == 0 {
		return "", false
	}
	normalizedText := normalizeKeywordComparable(text)
	index, ok := m.firstMatchNormalized(normalizedText)
	if !ok || index < 0 || int(index) >= len(m.keywords) {
		return "", false
	}
	return m.keywords[index], true
}

const contentModerationKeywordInlineMatches = 8

type contentModerationKeywordMatchAccumulator struct {
	best                 int32
	hitCount             int
	expandedGroupCount   int
	collectAll           bool
	hitWords             []uint64
	expandedGroupWords   []uint64
	inlineHits           [contentModerationKeywordInlineMatches]int32
	inlineExpandedGroups [contentModerationKeywordInlineMatches]int32
}

func (a *contentModerationKeywordMatchAccumulator) groupExpanded(group int32) bool {
	if !a.collectAll || group < 0 {
		return false
	}
	if len(a.expandedGroupWords) == 0 {
		for index := 0; index < a.expandedGroupCount; index++ {
			if a.inlineExpandedGroups[index] == group {
				return true
			}
		}
		return false
	}
	wordIndex := int(group) >> 6
	if wordIndex >= len(a.expandedGroupWords) {
		return false
	}
	return a.expandedGroupWords[wordIndex]&(uint64(1)<<(uint(group)&63)) != 0
}

func (a *contentModerationKeywordMatchAccumulator) markGroupExpanded(group int32, groupCount int) {
	if !a.collectAll || group < 0 {
		return
	}
	if len(a.expandedGroupWords) == 0 && a.expandedGroupCount < len(a.inlineExpandedGroups) {
		a.inlineExpandedGroups[a.expandedGroupCount] = group
		a.expandedGroupCount++
		return
	}
	if len(a.expandedGroupWords) == 0 {
		a.expandedGroupWords = make([]uint64, (groupCount+63)/64)
		for index := 0; index < a.expandedGroupCount; index++ {
			existingGroup := a.inlineExpandedGroups[index]
			a.expandedGroupWords[int(existingGroup)>>6] |= uint64(1) << (uint(existingGroup) & 63)
		}
	}
	wordIndex := int(group) >> 6
	if wordIndex >= len(a.expandedGroupWords) {
		return
	}
	a.expandedGroupWords[wordIndex] |= uint64(1) << (uint(group) & 63)
	a.expandedGroupCount++
}

func (a *contentModerationKeywordMatchAccumulator) add(keywordIndex int32, keywordCount int) {
	if keywordIndex < 0 {
		return
	}
	if a.best < 0 || keywordIndex < a.best {
		a.best = keywordIndex
	}
	if !a.collectAll {
		return
	}
	if len(a.hitWords) == 0 {
		for index := 0; index < a.hitCount; index++ {
			if a.inlineHits[index] == keywordIndex {
				return
			}
		}
		if a.hitCount < len(a.inlineHits) {
			a.inlineHits[a.hitCount] = keywordIndex
			a.hitCount++
			return
		}
		a.hitWords = make([]uint64, (keywordCount+63)/64)
		for index := 0; index < a.hitCount; index++ {
			existingKeyword := a.inlineHits[index]
			a.hitWords[int(existingKeyword)>>6] |= uint64(1) << (uint(existingKeyword) & 63)
		}
	}
	wordIndex := int(keywordIndex) >> 6
	if wordIndex >= len(a.hitWords) {
		return
	}
	mask := uint64(1) << (uint(keywordIndex) & 63)
	if a.hitWords[wordIndex]&mask == 0 {
		a.hitWords[wordIndex] |= mask
		a.hitCount++
	}
}

func (a *contentModerationKeywordMatchAccumulator) hasKeyword(keywordIndex int32) bool {
	if keywordIndex < 0 || len(a.hitWords) == 0 {
		return false
	}
	wordIndex := int(keywordIndex) >> 6
	return wordIndex < len(a.hitWords) &&
		a.hitWords[wordIndex]&(uint64(1)<<(uint(keywordIndex)&63)) != 0
}

func (a *contentModerationKeywordMatchAccumulator) sortInlineHits() {
	for index := 1; index < a.hitCount; index++ {
		current := a.inlineHits[index]
		insertAt := index
		for insertAt > 0 && current < a.inlineHits[insertAt-1] {
			a.inlineHits[insertAt] = a.inlineHits[insertAt-1]
			insertAt--
		}
		a.inlineHits[insertAt] = current
	}
}

func (m *contentModerationKeywordMatcher) firstMatchNormalized(normalizedText string) (int32, bool) {
	if m == nil || normalizedText == "" || len(m.nodes) == 0 || len(m.keywords) == 0 {
		return -1, false
	}
	accumulator := contentModerationKeywordMatchAccumulator{best: -1}
	m.scanNormalized(normalizedText, &accumulator)
	return accumulator.best, accumulator.best >= 0
}

func (m *contentModerationKeywordMatcher) scanNormalized(
	normalizedText string,
	accumulator *contentModerationKeywordMatchAccumulator,
) {
	directState := int32(0)
	compactState := int32(0)
	compactContiguousBytes := 0
	for index := 0; index < len(normalizedText); index++ {
		label := normalizedText[index]
		directState = m.advance(directState, label)
		m.recordOutputs(
			directState,
			contentModerationKeywordModeDirect,
			normalizedText,
			index+1,
			0,
			accumulator,
		)
		if m.matchCollectionComplete(accumulator) {
			return
		}
		if !m.hasCompact || label == ' ' {
			if label == ' ' {
				compactContiguousBytes = 0
			}
			continue
		}
		compactContiguousBytes++
		compactState = m.advance(compactState, label)
		m.recordOutputs(
			compactState,
			contentModerationKeywordModeCompact,
			normalizedText,
			index+1,
			compactContiguousBytes,
			accumulator,
		)
		if m.matchCollectionComplete(accumulator) {
			return
		}
	}
}

func (m *contentModerationKeywordMatcher) matchCollectionComplete(
	accumulator *contentModerationKeywordMatchAccumulator,
) bool {
	if accumulator.collectAll {
		return accumulator.hitCount == m.matchableKeywordCount
	}
	return accumulator.best == m.minimumKeyword
}

func (m *contentModerationKeywordMatcher) advance(state int32, label byte) int32 {
	for {
		next := m.next(state, label)
		if next != 0 || state == 0 {
			return next
		}
		state = m.nodes[state].failure
	}
}

func (m *contentModerationKeywordMatcher) recordOutputs(
	state int32,
	mode uint8,
	normalizedText string,
	end int,
	compactContiguousBytes int,
	accumulator *contentModerationKeywordMatchAccumulator,
) {
	if !keywordComparableEndBoundaryAt(normalizedText, end) {
		return
	}
	for nodeIndex := state; nodeIndex >= 0; nodeIndex = m.nodes[nodeIndex].outputLink {
		node := m.nodes[nodeIndex]
		minimumKeyword := node.directMin
		group := node.directGroup
		if mode == contentModerationKeywordModeCompact {
			minimumKeyword = node.compactMin
			group = node.compactGroup
		}
		if minimumKeyword < 0 || accumulator.groupExpanded(group) {
			continue
		}
		patternBytes := int(node.patternBytes)
		start := end - patternBytes
		if mode == contentModerationKeywordModeCompact && compactContiguousBytes < patternBytes {
			var ok bool
			start, ok = compactKeywordMatchStart(normalizedText, end, patternBytes)
			if !ok {
				continue
			}
		}
		if start < 0 || !keywordComparableStartBoundaryAt(normalizedText, start) {
			continue
		}
		if !accumulator.collectAll {
			accumulator.add(minimumKeyword, len(m.keywords))
			continue
		}
		accumulator.markGroupExpanded(group, m.outputGroupCount)
		for outputIndex := node.terminalHead; outputIndex >= 0; outputIndex = m.outputs[outputIndex].next {
			output := m.outputs[outputIndex]
			if output.modes&mode != 0 {
				accumulator.add(output.keywordIndex, len(m.keywords))
			}
		}
	}
}

func compactKeywordMatchStart(normalizedText string, end, patternBytes int) (int, bool) {
	start := end
	remaining := patternBytes
	for start > 0 && remaining > 0 {
		start--
		if normalizedText[start] != ' ' {
			remaining--
		}
	}
	return start, remaining == 0
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
