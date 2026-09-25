package dify

import (
	"context"
	"net/http"
)

// Audio is text to speech, and speech to text, for this app.
type Audio struct{ t *transport }

// SpeakParams choose what is spoken and how.
type SpeakParams struct {
	// Text to speak. Either this or MessageID.
	Text string
	// MessageID speaks an existing message instead.
	MessageID string
	// Voice is often required: an app with no default voice configured
	// answers "TTS is not enabled" without one. The app's own voice, when it
	// has one, is in AppParameters.Raw["text_to_speech"]["voice"].
	Voice string
	User  string
}

// Speak turns text, or an existing message, into audio, and returns the
// audio with the headers that say its format.
func (a *Audio) Speak(ctx context.Context, p SpeakParams) ([]byte, http.Header, error) {
	if p.Text == "" && p.MessageID == "" {
		return nil, nil, argError("nothing to speak; set SpeakParams.Text or SpeakParams.MessageID")
	}
	user, err := a.t.who(p.User)
	if err != nil {
		return nil, nil, err
	}
	body := map[string]any{"user": user}
	if p.Text != "" {
		body["text"] = p.Text
	}
	if p.MessageID != "" {
		body["message_id"] = p.MessageID
	}
	if p.Voice != "" {
		body["voice"] = p.Voice
	}
	return a.t.bytes(ctx, &request{method: http.MethodPost, path: "/text-to-audio", body: body})
}

// Transcribe turns recorded speech into text.
func (a *Audio) Transcribe(ctx context.Context, file Upload, user string) (string, error) {
	who, err := a.t.who(user)
	if err != nil {
		return "", err
	}
	part, err := file.part("file", false)
	if err != nil {
		return "", err
	}
	o, err := a.t.call(ctx, &request{method: http.MethodPost, path: "/audio-to-text", form: &multipartForm{fields: map[string]string{"user": who}, file: part}})
	if err != nil {
		return "", err
	}
	return o.str("text"), nil
}
