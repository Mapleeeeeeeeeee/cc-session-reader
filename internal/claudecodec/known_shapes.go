package claudecodec

// Content block types inside message.content. The reader acts on these four
// at the top level of a message, and on text alone inside a tool_result's own
// content array; every other block type is skipped without a trace.
const (
	blockTypeText       = "text"
	blockTypeThinking   = "thinking"
	blockTypeToolUse    = "tool_use"
	blockTypeToolResult = "tool_result"
)

const (
	recordTypeSystem        = "system"
	systemSubtypeCompaction = "compact_boundary"
)

// handledContentBlockTypes are the message.content block types the reader
// reads (see rawMessage.Text, rawMessage.Assistant and extractToolResultText).
var handledContentBlockTypes = map[string]bool{
	blockTypeText:       true,
	blockTypeThinking:   true,
	blockTypeToolUse:    true,
	blockTypeToolResult: true,
}

// handledToolResultBlockTypes are the block types the reader reads inside a
// tool_result block's content array (see extractToolResultText).
var handledToolResultBlockTypes = map[string]bool{
	blockTypeText: true,
}

// knownAttachmentTypes are the attachment.type values seen in real
// transcripts. The reader drops every attachment record as noise without
// looking at this field, so the list exists only for `drift`: a type missing
// from it is reported as new. To acknowledge a new type, add it here.
var knownAttachmentTypes = map[string]bool{
	"advisor_tool":              true,
	"agent_listing_delta":       true,
	"agent_mention":             true,
	"already_read_file":         true,
	"async_hook_response":       true,
	"auto_mode":                 true,
	"bash_output_audience_note": true,
	"batching_reminder_sent":    true,
	"budget_usd":                true,
	"command_permissions":       true,
	"compact_file_reference":    true,
	"credential_org":            true,
	"critical_system_reminder":  true,
	"date":                      true,
	"date_change":               true,
	"deferred_tools_delta":      true,
	"deferred_tools_record":     true,
	"diagnostics":               true,
	"directory":                 true,
	"dynamic_skill":             true,
	"edited_text_file":          true,
	"environment":               true,
	"file":                      true,
	"fork_briefing":             true,
	"goal_status":               true,
	"hook_additional_context":   true,
	"hook_blocking_error":       true,
	"hook_cancelled":            true,
	"hook_non_blocking_error":   true,
	"hook_permission_decision":  true,
	"hook_success":              true,
	"hook_system_message":       true,
	"inlined_image_paths":       true,
	"instructions":              true,
	"invoked_skills":            true,
	"language":                  true,
	"max_turns_reached":         true,
	"mcp_instructions_delta":    true,
	"model":                     true,
	"nested_memory":             true,
	"opened_file_in_ide":        true,
	"output_style":              true,
	"output_style_instructions": true,
	"plan_mode":                 true,
	"plan_mode_exit":            true,
	"prompt_snapshot":           true,
	"queued_command":            true,
	"read_truncation_notice":    true,
	"remote_session_change":     true,
	"sandbox_instructions":      true,
	"selected_lines_in_ide":     true,
	"session_context":           true,
	"session_cron_carry":        true,
	"silent_turn_reminder":      true,
	"skill_listing":             true,
	"skill_mention":             true,
	"structured_output":         true,
	"task_reminder":             true,
	"task_status":               true,
	"thinking_drop":             true,
	"thinking_stripped":         true,
	"total_tokens_reminder":     true,
	"ultrathink_effort":         true,
}

// knownSystemSubtypes are the subtype values of system records seen in real
// transcripts. The reader drops system records as noise except
// compact_boundary, which it turns into a compaction marker; the remaining
// entries exist only for `drift`. To acknowledge a new subtype, add it here.
var knownSystemSubtypes = map[string]bool{
	systemSubtypeCompaction:     true,
	"agents_killed":             true,
	"api_error":                 true,
	"away_summary":              true,
	"bridge_status":             true,
	"informational":             true,
	"local_command":             true,
	"model_refusal_fallback":    true,
	"model_refusal_no_fallback": true,
	"permission_retry":          true,
	"scheduled_task_fire":       true,
	"stop_hook_summary":         true,
	"turn_duration":             true,
}

// IsHandledRecordType reports whether the reader has an explicit rule for a
// top-level record type: it is conversation, listed noise, or a listed
// non-conversation type.
func IsHandledRecordType(recordType string) bool {
	return conversationTypes[recordType] || noiseTypes[recordType] || knownNonConversationTypes[recordType]
}

// IsConversationRecordType reports whether a record of this type can carry
// conversation, and so whether its content blocks are read at all.
func IsConversationRecordType(recordType string) bool {
	return conversationTypes[recordType]
}

// IsKnownAttachmentType reports whether attachmentType is in knownAttachmentTypes.
func IsKnownAttachmentType(attachmentType string) bool {
	return knownAttachmentTypes[attachmentType]
}

// IsKnownSystemSubtype reports whether subtype is in knownSystemSubtypes.
func IsKnownSystemSubtype(subtype string) bool {
	return knownSystemSubtypes[subtype]
}

// IsHandledContentBlockType reports whether the reader reads a block of this
// type directly inside message.content.
func IsHandledContentBlockType(blockType string) bool {
	return handledContentBlockTypes[blockType]
}

// IsHandledToolResultBlockType reports whether the reader reads a block of
// this type inside a tool_result block's content array.
func IsHandledToolResultBlockType(blockType string) bool {
	return handledToolResultBlockTypes[blockType]
}
