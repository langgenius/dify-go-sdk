package dify_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/langgenius/dify-go-sdk"
)

func Example() {
	ctx := context.Background()
	app, err := dify.NewApp(dify.WithAPIKey("app-…"), dify.WithUser("alice"))
	if err != nil {
		log.Fatal(err)
	}

	msg, err := app.Chat.Messages.Create(ctx, "Hello", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(msg.Answer, msg.ConversationID)
}

func ExampleWorkflowRuns_Stream() {
	ctx := context.Background()
	app, _ := dify.NewApp(dify.WithUser("alice")) // key from DIFY_API_KEY

	stream, err := app.Workflows.Runs.Stream(ctx, map[string]any{"text": "…"}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close() // stops watching; the run keeps going

	for event, err := range stream.Events() {
		if err != nil {
			log.Fatal(err) // includes an "error" event Dify sent after the 200
		}
		if event.Execution != nil {
			fmt.Println(event.Execution.NodeID, event.Execution.Status)
		}
	}
	run := stream.FinalRun()
	switch {
	case run.Paused():
		fmt.Println("waiting for a person:", run.PendingForms)
	case run.Succeeded():
		fmt.Println(run.Outputs, run.Usage())
	default:
		fmt.Println(run.Err())
	}
}

func ExampleMessageStream_Text() {
	ctx := context.Background()
	app, _ := dify.NewApp(dify.WithUser("alice"))

	stream, err := app.Chat.Messages.Stream(ctx, "Tell me a story", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	for piece, err := range stream.Text() {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(piece)
	}
	// Finished is false if the answer stopped arriving halfway.
	fmt.Println(stream.FinalMessage().Finished)
}

func ExamplePage_All() {
	ctx := context.Background()
	app, _ := dify.NewApp(dify.WithUser("alice"))

	page, err := app.Chat.Conversations.List(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	for conversation, err := range page.All(ctx) {
		var limit *dify.PageLimitError
		if errors.As(err, &limit) {
			break // the listing kept going past dify.MaxWalk items
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(conversation.ID, conversation.Name)
	}
}

func ExampleForms_Submit() {
	ctx := context.Background()
	app, _ := dify.NewApp(dify.WithUser("alice"))

	run, _ := app.Workflows.Runs.Create(ctx, map[string]any{"doc": "…"}, nil)
	if run.Paused() {
		token, err := dify.FormToken(run)
		if err != nil {
			log.Fatal(err) // paused on a form meant for Dify's own UI
		}
		form, _ := app.Forms.Retrieve(ctx, token)
		_ = app.Forms.Submit(ctx, token, map[string]any{"decision": "approve"}, fmt.Sprint(form.Actions[0]["id"]), "")

		resumed, _ := app.Workflows.Runs.Events(ctx, run.RunID, nil)
		for range resumed.Events() {
		}
		fmt.Println(resumed.FinalRun().Status)
	}
}

func ExampleAPIError() {
	ctx := context.Background()
	app, _ := dify.NewApp(dify.WithAPIKey("app-wrong"))

	_, err := app.Info(ctx)
	var apiErr *dify.APIError
	switch {
	case errors.Is(err, dify.ErrAuthentication):
		fmt.Println("check the key")
	case errors.Is(err, dify.ErrRateLimited) && errors.As(err, &apiErr):
		fmt.Println("retry after", apiErr.RetryAfter)
	case errors.As(err, &apiErr):
		fmt.Println(apiErr.Message, apiErr.ServerVersion)
	}
}

func ExampleWithTimeout() {
	// A deadline on the context replaces the client's timeout for that call,
	// so one long run gets twenty minutes without every call getting them.
	app, _ := dify.NewApp(dify.WithUser("alice"), dify.WithTimeout(30*time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	_, _ = app.Workflows.Runs.Create(ctx, map[string]any{"batch": "…"}, nil)
}

func ExampleApps_Deploy() {
	ctx := context.Background()
	m, err := dify.LoginManagement(ctx, os.Getenv("DIFY_CONSOLE_EMAIL"), os.Getenv("DIFY_CONSOLE_PASSWORD"), dify.WithHost("http://localhost"))
	if err != nil {
		log.Fatal(err)
	}
	dsl, _ := os.ReadFile("support-bot.yml")

	d, err := m.Apps.Deploy(ctx, string(dsl), nil)
	if err != nil {
		log.Fatal(err) // an argument refused before anything was sent
	}
	if err := d.Err(dify.StageRunnable); err != nil {
		// Where it stopped, and what is left on Dify: an app imported but not
		// published, an import held for confirmation, or one whose answer
		// never arrived and may exist.
		log.Fatal(err)
	}

	app, _ := m.AppClient(d.APIKey, "alice")
	msg, err := app.Chat.Messages.Create(ctx, "Hello", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(msg.Answer)
}
