package promptfilter

type literalPrefilterNode struct {
	next    map[byte]int
	failure int
	outputs []int
}

type literalPrefilter struct {
	nodes        []literalPrefilterNode
	literalCount int
}

type requiredLiteralCondition struct {
	literalID int
	all       bool
	children  []*requiredLiteralCondition
}

type literalPrefilterBuilder struct {
	nodes      []literalPrefilterNode
	literalIDs map[string]int
}

func newLiteralPrefilterBuilder() *literalPrefilterBuilder {
	return &literalPrefilterBuilder{
		nodes:      []literalPrefilterNode{{next: make(map[byte]int)}},
		literalIDs: make(map[string]int),
	}
}

func (b *literalPrefilterBuilder) addCondition(condition *requiredTextCondition) *requiredLiteralCondition {
	if condition == nil {
		return nil
	}
	if condition.literal != "" {
		id, ok := b.literalIDs[condition.literal]
		if !ok {
			id = len(b.literalIDs)
			b.literalIDs[condition.literal] = id
			b.addLiteral(condition.literal, id)
		}
		return &requiredLiteralCondition{literalID: id}
	}
	children := make([]*requiredLiteralCondition, 0, len(condition.children))
	for _, child := range condition.children {
		if compiled := b.addCondition(child); compiled != nil {
			children = append(children, compiled)
		}
	}
	if len(children) == 0 {
		return nil
	}
	if len(children) == 1 {
		return children[0]
	}
	return &requiredLiteralCondition{literalID: -1, all: condition.all, children: children}
}

func (b *literalPrefilterBuilder) addLiteral(literal string, id int) {
	state := 0
	for index := 0; index < len(literal); index++ {
		label := literal[index]
		next, ok := b.nodes[state].next[label]
		if !ok {
			next = len(b.nodes)
			b.nodes = append(b.nodes, literalPrefilterNode{next: make(map[byte]int)})
			b.nodes[state].next[label] = next
		}
		state = next
	}
	b.nodes[state].outputs = append(b.nodes[state].outputs, id)
}

func (b *literalPrefilterBuilder) build() *literalPrefilter {
	if len(b.literalIDs) == 0 {
		return nil
	}
	queue := make([]int, 0, len(b.nodes))
	for _, state := range b.nodes[0].next {
		queue = append(queue, state)
	}
	for head := 0; head < len(queue); head++ {
		state := queue[head]
		for label, next := range b.nodes[state].next {
			queue = append(queue, next)
			failure := b.nodes[state].failure
			for failure != 0 {
				if candidate, ok := b.nodes[failure].next[label]; ok {
					failure = candidate
					break
				}
				failure = b.nodes[failure].failure
			}
			if failure == 0 {
				if candidate, ok := b.nodes[0].next[label]; ok && candidate != next {
					failure = candidate
				}
			}
			b.nodes[next].failure = failure
			b.nodes[next].outputs = append(b.nodes[next].outputs, b.nodes[failure].outputs...)
		}
	}
	return &literalPrefilter{nodes: b.nodes, literalCount: len(b.literalIDs)}
}

func (p *literalPrefilter) match(text string) []bool {
	if p == nil || p.literalCount == 0 {
		return nil
	}
	hits := make([]bool, p.literalCount)
	state := 0
	for index := 0; index < len(text); index++ {
		label := text[index]
		for state != 0 {
			if _, ok := p.nodes[state].next[label]; ok {
				break
			}
			state = p.nodes[state].failure
		}
		if next, ok := p.nodes[state].next[label]; ok {
			state = next
		}
		for _, id := range p.nodes[state].outputs {
			hits[id] = true
		}
	}
	return hits
}

func (condition *requiredLiteralCondition) matches(hits []bool) bool {
	if condition == nil {
		return true
	}
	if len(condition.children) == 0 {
		return condition.literalID >= 0 && condition.literalID < len(hits) && hits[condition.literalID]
	}
	if condition.all {
		for _, child := range condition.children {
			if !child.matches(hits) {
				return false
			}
		}
		return true
	}
	for _, child := range condition.children {
		if child.matches(hits) {
			return true
		}
	}
	return false
}
