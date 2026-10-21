package service

import "strings"

// openAISSEDataFramer 为 OpenAI Chat Completions SSE 流补齐事件分隔空行。
//
// SSE 规范要求每个事件以空行结束。部分 OpenAI 兼容上游只发送
//
//	data: {"..."}
//	data: {"..."}
//
// 也就是只用单个 \n 分隔 data 行，严格客户端会把所有 data 行累积成同一个
// 未完成事件，直到 EOF 都收不到任何 chunk。该 framer 在每条 data 行后立即
// 写出一个空行，并吞掉上游随后本就存在的空行，因此：
//
//   - 单换行流：补成合规的 data: ...\n\n；
//   - 已合规流：输出字节保持不变；
//   - 非 SSE 裸 JSON：原样透传。
//
// 该实现与 raw CC 转发路径既有的“一行 data 一个完整 JSON chunk”假设一致。
type openAISSEDataFramer struct {
	emit      func(string)
	skipBlank bool
}

func newOpenAISSEDataFramer(emit func(string)) *openAISSEDataFramer {
	return &openAISSEDataFramer{emit: emit}
}

// Add 处理上游的一行。空行表示上游已经写出了事件分隔符；若上一行是
// data 行，则说明 framer 已经补过空行，直接丢弃这个重复空行。
func (f *openAISSEDataFramer) Add(line string) {
	if f == nil || f.emit == nil {
		return
	}
	if line == "" {
		if f.skipBlank {
			f.skipBlank = false
			return
		}
		f.emit("")
		return
	}

	f.emit(line)
	if strings.HasPrefix(line, "data:") {
		f.emit("")
		f.skipBlank = true
		return
	}
	f.skipBlank = false
}
