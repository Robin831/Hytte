package training

import "strings"

// Assistant replies that can change something (Stride's coach thread, the
// trip chat) share one convention: a conversational reply, optionally
// followed by one fenced JSON block carrying the structured change. These
// helpers split such a reply.

// ExtractFencedObject returns the content of the last fenced code block in a
// reply when it holds a JSON object.
func ExtractFencedObject(response string) (string, bool) {
	const fence = "```"
	lastClose := strings.LastIndex(response, "\n"+fence)
	if lastClose < 0 {
		return "", false
	}
	openStart := strings.LastIndex(response[:lastClose], fence)
	if openStart < 0 {
		return "", false
	}
	afterTag := response[openStart+len(fence):]
	nl := strings.IndexByte(afterTag, '\n')
	if nl < 0 {
		return "", false
	}
	innerStart := openStart + len(fence) + nl + 1
	innerEnd := lastClose
	if innerEnd > innerStart && response[innerEnd-1] == '\r' {
		innerEnd--
	}
	if innerEnd <= innerStart {
		return "", false
	}
	inner := strings.TrimSpace(response[innerStart:innerEnd])
	if !strings.HasPrefix(inner, "{") {
		return "", false
	}
	return inner, true
}

// SplitFencedObject separates the conversational text from a trailing fenced
// JSON object. Without one, the whole reply is text.
func SplitFencedObject(response string) (text, block string, ok bool) {
	block, ok = ExtractFencedObject(response)
	if !ok {
		return strings.TrimSpace(response), "", false
	}
	const fence = "```"
	lastClose := strings.LastIndex(response, "\n"+fence)
	openStart := strings.LastIndex(response[:lastClose], fence)
	return strings.TrimSpace(response[:openStart]), block, true
}
