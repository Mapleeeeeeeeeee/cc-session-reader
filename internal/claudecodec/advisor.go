package claudecodec

import (
	"encoding/json"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// Values of advisor_tool_result.content.type seen in real transcripts. The
// advisor's reply is never stored as readable text: it is either encrypted
// (the vast majority) or an error.
const (
	advisorResultTypeRedacted = "advisor_redacted_result"
	advisorResultTypeError    = "advisor_tool_result_error"
)

// advisorRedactedResultText stands in for an encrypted advisor reply, so the
// summary and `expand` say the reply is unavailable instead of printing the
// base64 payload.
const advisorRedactedResultText = "response is encrypted in the transcript"

type advisorResultContent struct {
	Type      string `json:"type"`
	ErrorCode string `json:"error_code"`
}

// advisorToolResultBlock returns the advisor_tool_result block of an assistant
// message. Claude Code writes each content block of an assistant message as
// its own record, so the reply arrives as an assistant record rather than as
// a user record carrying toolUseResult.
func (m rawMessage) advisorToolResultBlock() (rawContentBlock, bool) {
	for _, block := range m.Blocks {
		if block.Type == blockTypeAdvisorToolResult {
			return block, true
		}
	}
	return rawContentBlock{}, false
}

// toAdvisorToolResult turns the reply into the same ToolResult a client-side
// tool produces, so it flows through the existing summary and expand paths.
// A content type the reader has no rule for keeps its raw JSON as the text
// rather than being dropped.
func (b rawContentBlock) toAdvisorToolResult() session.ToolResult {
	result := session.ToolResult{
		ToolUseID: b.ToolUseID,
		RawName:   session.ToolAdvisor,
		Success:   true,
	}
	var content advisorResultContent
	_ = json.Unmarshal(b.Content, &content)
	switch content.Type {
	case advisorResultTypeRedacted:
		result.Text = advisorRedactedResultText
	case advisorResultTypeError:
		result.Success = false
		result.Text = content.ErrorCode
	default:
		result.Text = marshalNoEscape(b.Content)
	}
	return result
}
