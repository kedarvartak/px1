package learning

import (
	"context"
	"testing"

	"px1/internal/hosted/llm"
)

type replyModelFunc func(context.Context, llm.Request) (llm.Response, error)

func (f replyModelFunc) Complete(ctx context.Context, request llm.Request) (llm.Response, error) {
	return f(ctx, request)
}

type replyStoreFunc func(context.Context, ReplyInput, ReplyClassification) error

func (f replyStoreFunc) SaveReplyClassification(ctx context.Context, input ReplyInput, result ReplyClassification) error {
	return f(ctx, input, result)
}

type threadReplierFunc func(context.Context, int64, int64, string) error

func (f threadReplierFunc) ReplyToComment(ctx context.Context, installationID, commentID int64, body string) error {
	return f(ctx, installationID, commentID, body)
}

func TestReplyProcessorClassifiesAndAnswersQuestion(t *testing.T) {
	stored, answered := false, false
	processor := ReplyProcessor{
		Provider: "openai", ModelName: "cheap-model",
		Model: replyModelFunc(func(_ context.Context, request llm.Request) (llm.Response, error) {
			if request.MaxOutputTokens > 300 || request.Temperature != 0 {
				t.Fatalf("unbounded request: %+v", request)
			}
			return llm.Response{Text: `{"kind":"question","confidence":0.9,"answer":"The evidence came from line 12."}`}, nil
		}),
		Store: replyStoreFunc(func(_ context.Context, _ ReplyInput, result ReplyClassification) error {
			stored = result.Kind == "question"
			return nil
		}),
		Replier: threadReplierFunc(func(_ context.Context, installationID, commentID int64, body string) error {
			answered = installationID == 1 && commentID == 4 && body != ""
			return nil
		}),
	}
	_, err := processor.Process(context.Background(), ReplyInput{InstallationID: 1, RepositoryID: 2, FindingID: "f", CommentID: 4, ActorID: 5, Finding: "line 12 can panic", Reply: "Why is this a bug?"})
	if err != nil {
		t.Fatal(err)
	}
	if !stored || !answered {
		t.Fatalf("stored=%v answered=%v", stored, answered)
	}
}

func TestReplyClassificationRejectsAnswerForNonQuestion(t *testing.T) {
	if _, err := decodeReplyClassification([]byte(`{"kind":"agree","confidence":0.8,"answer":"extra"}`)); err == nil {
		t.Fatal("accepted answer for non-question")
	}
}
