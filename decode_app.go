package dify

// formTokens reads the forms a paused blocking answer is waiting on, from
// data.reasons[].form_token.
func formTokens(payload object) []string {
	source := payload
	if data := payload.obj("data"); data.has("reasons") {
		source = data
	}
	var tokens []string
	for _, reason := range source.objs("reasons") {
		if token := reason.str("form_token"); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// pausedNodes reads the nodes a paused blocking answer is waiting at.
func pausedNodes(payload object) []string {
	source := payload
	if data := payload.obj("data"); data.has("reasons") {
		source = data
	}
	var nodes []string
	for _, reason := range source.objs("reasons") {
		if node := reason.str("node_id"); node != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func serverInfoFrom(o object) *ServerInfo {
	return &ServerInfo{ServerVersion: o.str("server_version"), APIVersion: o.str("api_version"), Welcome: o.str("welcome")}
}

// inputField unwraps one {"text-input": {...}} entry. Dify keys each field by
// its kind rather than putting the kind inside, and an entry with no name is
// skipped rather than turned into a field called "".
func inputField(entry object) (InputField, bool) {
	for kind, config := range entry {
		cfg, ok := config.(map[string]any)
		if !ok {
			continue
		}
		c := object(cfg)
		name := c.str("variable")
		if name == "" {
			continue
		}
		f := InputField{
			Name:        name,
			Label:       c.str("label"),
			Type:        firstNonZero(c.str("type"), kind),
			Required:    c.bool("required"),
			Options:     c.strs("options"),
			Default:     c["default"],
			Description: c.str("description"),
			Hidden:      c.bool("hide"),
			Raw:         c.raw(),
		}
		if c.has("max_length") {
			n := c.int("max_length")
			f.MaxLength = &n
		}
		return f, true
	}
	return InputField{}, false
}

func annotationFrom(o object) Annotation {
	return Annotation{
		ID:        o.str("id"),
		Question:  o.str("question"),
		Answer:    o.str("answer"),
		HitCount:  o.int("hit_count"),
		CreatedAt: o.intPtr("created_at"),
		Raw:       o.raw(),
	}
}

func jobFrom(o object, id string) *AnnotationReplyJob {
	return &AnnotationReplyJob{
		ID:     firstNonZero(id, firstNonZero(o.str("job_id"), o.str("id"))),
		Status: firstNonZero(o.str("job_status"), o.str("status")),
		Error:  o.str("error_msg"),
	}
}

func conversationFrom(o object) Conversation {
	return Conversation{
		ID:           o.str("id"),
		Name:         o.str("name"),
		Status:       o.str("status"),
		Introduction: o.str("introduction"),
		Inputs:       o.obj("inputs").raw(),
		CreatedAt:    o.intPtr("created_at"),
		UpdatedAt:    o.intPtr("updated_at"),
		Raw:          o.raw(),
	}
}

func uploadedFrom(o object) *UploadedFile {
	return &UploadedFile{
		ID:        o.str("id"),
		Name:      o.str("name"),
		Size:      o.int64("size"),
		MimeType:  o.str("mime_type"),
		Extension: o.str("extension"),
		CreatedBy: o.str("created_by"),
		CreatedAt: o.intPtr("created_at"),
		Raw:       o.raw(),
	}
}

// messageFromBlocking reads a blocking reply, which is the finished message
// by definition — except when a chatflow reaches a human-input node and
// answers workflow_paused with the forms it is waiting on. That is a message
// still being written, and calling it finished would make Succeeded true for
// an answer nobody has given yet.
func messageFromBlocking(o object) *Message {
	waiting := formTokens(o)
	return &Message{
		Finished:       len(waiting) == 0,
		PendingForms:   waiting,
		Answer:         o.str("answer"),
		MessageID:      firstNonZero(o.str("message_id"), o.str("id")),
		ConversationID: o.str("conversation_id"),
		TaskID:         o.str("task_id"),
		Metadata:       o.obj("metadata").raw(),
		CreatedAt:      o.intPtr("created_at"),
		Raw:            o.raw(),
	}
}

func historyFrom(o object) HistoryMessage {
	return HistoryMessage{
		ID:                 o.str("id"),
		ConversationID:     o.str("conversation_id"),
		Query:              o.str("query"),
		Answer:             o.str("answer"),
		Inputs:             o.obj("inputs").raw(),
		Files:              o.maps("message_files"),
		Feedback:           o.obj("feedback").str("rating"),
		RetrieverResources: o.maps("retriever_resources"),
		AgentThoughts:      o.maps("agent_thoughts"),
		Status:             o.str("status"),
		Error:              o.str("error"),
		CreatedAt:          o.intPtr("created_at"),
		Usage: usageFrom(object{
			"prompt_tokens":     o["message_tokens"],
			"completion_tokens": o["answer_tokens"],
			"total_tokens":      o["total_tokens"],
			"total_price":       o["total_price"],
			"currency":          o["currency"],
		}, o["provider_response_latency"]),
		Raw: o.raw(),
	}
}

// runFromBlocking reads a blocking response, which nests the run under data.
// Its token total is kept as ReportedUsage rather than invented into a
// nameless node execution.
func runFromBlocking(o object) *WorkflowRun {
	data := o.obj("data")
	if len(data) == 0 {
		data = o
	}
	run := &WorkflowRun{
		Status:  firstNonZero(data.str("status"), "unknown"),
		Outputs: data.obj("outputs").raw(),
		Nodes:   map[string]NodeExecution{},
		Error:   data.str("error"),
		RunID:   firstNonZero(data.str("id"), o.str("workflow_run_id")),
		TaskID:  o.str("task_id"),
		// A run that pauses answers blocking too, with the tokens to resume
		// it under data.reasons.
		PendingForms: formTokens(o),
		PausedNodes:  pausedNodes(o),
		Raw:          o.raw(),
	}
	if data.int("total_tokens") != 0 || truthy(data["total_price"]) {
		u := usageFrom(data, data["elapsed_time"])
		run.ReportedUsage = &u
	}
	return run
}

func appInfoFrom(o object) *AppInfo {
	return &AppInfo{
		Name:        o.str("name"),
		Mode:        o.str("mode"),
		Description: o.str("description"),
		Tags:        o.strs("tags"),
		AuthorName:  o.str("author_name"),
		Raw:         o.raw(),
	}
}

// parametersFrom reads features off whichever top-level blocks carry an
// "enabled" flag, since Dify adds features without announcing them.
func parametersFrom(o object) *AppParameters {
	p := &AppParameters{
		OpeningStatement:   o.str("opening_statement"),
		SuggestedQuestions: o.strs("suggested_questions"),
		Features:           map[string]bool{},
		FileUpload:         o.obj("file_upload").raw(),
		SystemParameters:   o.obj("system_parameters").raw(),
		Raw:                o.raw(),
	}
	for _, entry := range o.objs("user_input_form") {
		if f, ok := inputField(entry); ok {
			p.Inputs = append(p.Inputs, f)
		}
	}
	for key, value := range o {
		if m, ok := value.(map[string]any); ok {
			if _, has := m["enabled"]; has {
				p.Features[key] = object(m).bool("enabled")
			}
		}
	}
	return p
}

func siteFrom(o object) *SiteSettings {
	return &SiteSettings{
		Title:                  o.str("title"),
		Description:            o.str("description"),
		Icon:                   o.str("icon"),
		IconType:               o.str("icon_type"),
		IconBackground:         o.str("icon_background"),
		IconURL:                o.str("icon_url"),
		DefaultLanguage:        o.str("default_language"),
		ChatColorTheme:         o.str("chat_color_theme"),
		ChatColorThemeInverted: o.bool("chat_color_theme_inverted"),
		InputPlaceholder:       o.str("input_placeholder"),
		Copyright:              o.str("copyright"),
		PrivacyPolicy:          o.str("privacy_policy"),
		CustomDisclaimer:       o.str("custom_disclaimer"),
		ShowWorkflowSteps:      o.bool("show_workflow_steps"),
		UseIconAsAnswerIcon:    o.bool("use_icon_as_answer_icon"),
		Raw:                    o.raw(),
	}
}

// formFrom keeps the token the form was fetched by: Dify's answer does not
// repeat it, and it is what Submit needs.
func formFrom(o object, token string) *Form {
	return &Form{
		Token:     token,
		Content:   o.str("form_content"),
		Inputs:    o.maps("inputs"),
		Actions:   o.maps("user_actions"),
		Defaults:  o.obj("resolved_default_values").raw(),
		ExpiresAt: o.intPtr("expiration_time"),
		Raw:       o.raw(),
	}
}

func transcriptFrom(o object) string { return o.str("text") }

func suggestionsFrom(o object) []string { return o.strs("data") }
