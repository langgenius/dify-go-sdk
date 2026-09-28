package codec

import (
	"github.com/langgenius/dify-go-sdk/internal/entity"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
)

// formTokens reads the forms a paused blocking answer is waiting on, from
// data.reasons[].form_token.
func formTokens(payload kernel.Object) []string {
	source := payload
	if data := payload.Obj("data"); data.Has("reasons") {
		source = data
	}
	var tokens []string
	for _, reason := range source.Objs("reasons") {
		if token := reason.Str("form_token"); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// pausedNodes reads the nodes a paused blocking answer is waiting at.
func pausedNodes(payload kernel.Object) []string {
	source := payload
	if data := payload.Obj("data"); data.Has("reasons") {
		source = data
	}
	var nodes []string
	for _, reason := range source.Objs("reasons") {
		if node := reason.Str("node_id"); node != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func ServerInfoFrom(o kernel.Object) *entity.ServerInfo {
	return &entity.ServerInfo{ServerVersion: o.Str("server_version"), APIVersion: o.Str("api_version"), Welcome: o.Str("welcome")}
}

// inputField unwraps one {"text-input": {...}} entry. Dify keys each field by
// its kind rather than putting the kind inside, and an entry with no name is
// skipped rather than turned into a field called "".
func inputField(entry kernel.Object) (entity.InputField, bool) {
	for kind, config := range entry {
		cfg, ok := config.(map[string]any)
		if !ok {
			continue
		}
		c := kernel.Object(cfg)
		name := c.Str("variable")
		if name == "" {
			continue
		}
		f := entity.InputField{
			Name:        name,
			Label:       c.Str("label"),
			Type:        kernel.FirstNonZero(c.Str("type"), kind),
			Required:    c.Bool("required"),
			Options:     c.Strs("options"),
			Default:     c["default"],
			Description: c.Str("description"),
			Hidden:      c.Bool("hide"),
			Raw:         c.Raw(),
		}
		if c.Has("max_length") {
			n := c.Int("max_length")
			f.MaxLength = &n
		}
		return f, true
	}
	return entity.InputField{}, false
}

func AnnotationFrom(o kernel.Object) entity.Annotation {
	return entity.Annotation{
		ID:        o.Str("id"),
		Question:  o.Str("question"),
		Answer:    o.Str("answer"),
		HitCount:  o.Int("hit_count"),
		CreatedAt: o.IntPtr("created_at"),
		Raw:       o.Raw(),
	}
}

func JobFrom(o kernel.Object, id string) *entity.AnnotationReplyJob {
	return &entity.AnnotationReplyJob{
		ID:     kernel.FirstNonZero(id, kernel.FirstNonZero(o.Str("job_id"), o.Str("id"))),
		Status: kernel.FirstNonZero(o.Str("job_status"), o.Str("status")),
		Error:  o.Str("error_msg"),
	}
}

func ConversationFrom(o kernel.Object) entity.Conversation {
	return entity.Conversation{
		ID:           o.Str("id"),
		Name:         o.Str("name"),
		Status:       o.Str("status"),
		Introduction: o.Str("introduction"),
		Inputs:       o.Obj("inputs").Raw(),
		CreatedAt:    o.IntPtr("created_at"),
		UpdatedAt:    o.IntPtr("updated_at"),
		Raw:          o.Raw(),
	}
}

func UploadedFrom(o kernel.Object) *entity.UploadedFile {
	return &entity.UploadedFile{
		ID:        o.Str("id"),
		Name:      o.Str("name"),
		Size:      o.Int64("size"),
		MimeType:  o.Str("mime_type"),
		Extension: o.Str("extension"),
		CreatedBy: o.Str("created_by"),
		CreatedAt: o.IntPtr("created_at"),
		Raw:       o.Raw(),
	}
}

// MessageFromBlocking reads a blocking reply, which is the finished message
// by definition — except when a chatflow reaches a human-input node and
// answers workflow_paused with the forms it is waiting on. That is a message
// still being written, and calling it finished would make Succeeded true for
// an answer nobody has given yet.
func MessageFromBlocking(o kernel.Object) *entity.Message {
	waiting := formTokens(o)
	return &entity.Message{
		Finished:       len(waiting) == 0,
		PendingForms:   waiting,
		Answer:         o.Str("answer"),
		MessageID:      kernel.FirstNonZero(o.Str("message_id"), o.Str("id")),
		ConversationID: o.Str("conversation_id"),
		TaskID:         o.Str("task_id"),
		Metadata:       o.Obj("metadata").Raw(),
		CreatedAt:      o.IntPtr("created_at"),
		Raw:            o.Raw(),
	}
}

func HistoryFrom(o kernel.Object) entity.HistoryMessage {
	return entity.HistoryMessage{
		ID:                 o.Str("id"),
		ConversationID:     o.Str("conversation_id"),
		Query:              o.Str("query"),
		Answer:             o.Str("answer"),
		Inputs:             o.Obj("inputs").Raw(),
		Files:              o.Maps("message_files"),
		Feedback:           o.Obj("feedback").Str("rating"),
		RetrieverResources: o.Maps("retriever_resources"),
		AgentThoughts:      o.Maps("agent_thoughts"),
		Status:             o.Str("status"),
		Error:              o.Str("error"),
		CreatedAt:          o.IntPtr("created_at"),
		Usage: entity.UsageFrom(kernel.Object{
			"prompt_tokens":     o["message_tokens"],
			"completion_tokens": o["answer_tokens"],
			"total_tokens":      o["total_tokens"],
			"total_price":       o["total_price"],
			"currency":          o["currency"],
		}, o["provider_response_latency"]),
		Raw: o.Raw(),
	}
}

// RunFromBlocking reads a blocking response, which nests the run under data.
// Its token total is kept as ReportedUsage rather than invented into a
// nameless node execution.
func RunFromBlocking(o kernel.Object) *entity.WorkflowRun {
	data := o.Obj("data")
	if len(data) == 0 {
		data = o
	}
	run := &entity.WorkflowRun{
		Status:  kernel.FirstNonZero(data.Str("status"), "unknown"),
		Outputs: data.Obj("outputs").Raw(),
		Nodes:   map[string]entity.NodeExecution{},
		Error:   data.Str("error"),
		RunID:   kernel.FirstNonZero(data.Str("id"), o.Str("workflow_run_id")),
		TaskID:  o.Str("task_id"),
		// A run that pauses answers blocking too, with the tokens to resume
		// it under data.reasons.
		PendingForms: formTokens(o),
		PausedNodes:  pausedNodes(o),
		Raw:          o.Raw(),
	}
	if data.Int("total_tokens") != 0 || kernel.Truthy(data["total_price"]) {
		u := entity.UsageFrom(data, data["elapsed_time"])
		run.ReportedUsage = &u
	}
	return run
}

func AppInfoFrom(o kernel.Object) *entity.AppInfo {
	return &entity.AppInfo{
		Name:        o.Str("name"),
		Mode:        o.Str("mode"),
		Description: o.Str("description"),
		Tags:        o.Strs("tags"),
		AuthorName:  o.Str("author_name"),
		Raw:         o.Raw(),
	}
}

// ParametersFrom reads features off whichever top-level blocks carry an
// "enabled" flag, since Dify adds features without announcing them.
func ParametersFrom(o kernel.Object) *entity.AppParameters {
	p := &entity.AppParameters{
		OpeningStatement:   o.Str("opening_statement"),
		SuggestedQuestions: o.Strs("suggested_questions"),
		Features:           map[string]bool{},
		FileUpload:         o.Obj("file_upload").Raw(),
		SystemParameters:   o.Obj("system_parameters").Raw(),
		Raw:                o.Raw(),
	}
	for _, entry := range o.Objs("user_input_form") {
		if f, ok := inputField(entry); ok {
			p.Inputs = append(p.Inputs, f)
		}
	}
	for key, value := range o {
		if m, ok := value.(map[string]any); ok {
			if _, has := m["enabled"]; has {
				p.Features[key] = kernel.Object(m).Bool("enabled")
			}
		}
	}
	return p
}

func SiteFrom(o kernel.Object) *entity.SiteSettings {
	return &entity.SiteSettings{
		Title:                  o.Str("title"),
		Description:            o.Str("description"),
		Icon:                   o.Str("icon"),
		IconType:               o.Str("icon_type"),
		IconBackground:         o.Str("icon_background"),
		IconURL:                o.Str("icon_url"),
		DefaultLanguage:        o.Str("default_language"),
		ChatColorTheme:         o.Str("chat_color_theme"),
		ChatColorThemeInverted: o.Bool("chat_color_theme_inverted"),
		InputPlaceholder:       o.Str("input_placeholder"),
		Copyright:              o.Str("copyright"),
		PrivacyPolicy:          o.Str("privacy_policy"),
		CustomDisclaimer:       o.Str("custom_disclaimer"),
		ShowWorkflowSteps:      o.Bool("show_workflow_steps"),
		UseIconAsAnswerIcon:    o.Bool("use_icon_as_answer_icon"),
		Raw:                    o.Raw(),
	}
}

// FormFrom keeps the token the form was fetched by: Dify's answer does not
// repeat it, and it is what Submit needs.
func FormFrom(o kernel.Object, token string) *entity.Form {
	return &entity.Form{
		Token:     token,
		Content:   o.Str("form_content"),
		Inputs:    o.Maps("inputs"),
		Actions:   o.Maps("user_actions"),
		Defaults:  o.Obj("resolved_default_values").Raw(),
		ExpiresAt: o.IntPtr("expiration_time"),
		Raw:       o.Raw(),
	}
}

func TranscriptFrom(o kernel.Object) string { return o.Str("text") }

func SuggestionsFrom(o kernel.Object) []string { return o.Strs("data") }
