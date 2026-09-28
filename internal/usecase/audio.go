package usecase

import (
	"context"
	"net/http"

	"github.com/langgenius/dify-go-sdk/internal/codec"
	"github.com/langgenius/dify-go-sdk/internal/kernel"
	"github.com/langgenius/dify-go-sdk/internal/port"
)

// Audio is text to speech, and speech to text, for this app.
type Audio struct{ api port.Port }

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
		return nil, nil, kernel.ArgError("nothing to speak; set SpeakParams.Text or SpeakParams.MessageID")
	}
	user, err := a.api.Who(p.User)
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
	return a.api.Bytes(ctx, &port.Request{Method: http.MethodPost, Path: "/text-to-audio", Body: body})
}

// Transcribe turns recorded speech into text.
func (a *Audio) Transcribe(ctx context.Context, file Upload, user string) (string, error) {
	who, err := a.api.Who(user)
	if err != nil {
		return "", err
	}
	part, err := file.part("file", false)
	if err != nil {
		return "", err
	}
	o, err := a.api.Call(ctx, &port.Request{Method: http.MethodPost, Path: "/audio-to-text", Form: &port.MultipartForm{Fields: map[string]string{"user": who}, File: part}})
	if err != nil {
		return "", err
	}
	return codec.TranscriptFrom(o), nil
}
