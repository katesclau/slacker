package openaiadapter

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"time"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/sirupsen/logrus"
	"google.golang.org/adk/v2/model"
)

const (
	requestTimeout  = 90 * time.Second
	maxLLMRetries   = 3
	initialBackoff  = time.Second
	backoffMultiple = 2
)

var _ model.LLM = (*ModelAdapter)(nil)

type ModelAdapter struct {
	responses    responses.ResponseService
	defaultModel string
}

func New(apiKey string, defaultModel string) (*ModelAdapter, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	if defaultModel == "" {
		defaultModel = "gpt-5"
	}
	respsc := responses.NewResponseService(
		option.WithAPIKey(apiKey),
		option.WithEnvironmentProduction(),
		option.WithRequestTimeout(requestTimeout),
		option.WithBaseURL("https://us.api.openai.com/v1"),
	)
	return &ModelAdapter{
		responses:    respsc,
		defaultModel: defaultModel,
	}, nil
}

func (a *ModelAdapter) Name() string {
	return a.defaultModel
}

func (a *ModelAdapter) GenerateContent(ctx context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		responsesReq := responses.ResponseNewParams{
			Instructions: param.NewOpt(getSystemInstructions(req)),
			Input:        getRequestInput(req),
			Model:        a.resolveModel(ctx, req),
			Text:         responses.ResponseTextConfigParam{},
			Tools:        getRequestTools(req),
		}

		backoff := initialBackoff
		var resp *responses.Response
		var err error
		for attempt := range maxLLMRetries + 1 {
			resp, err = a.responses.New(ctx, responsesReq)
			if err == nil {
				break
			}
			if ctx.Err() != nil || attempt >= maxLLMRetries || !isRetryableError(err) {
				break
			}
			logrus.WithContext(ctx).WithError(err).Warnf("openai request failed (attempt %d/%d), retrying", attempt+1, maxLLMRetries+1)
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= backoffMultiple
		}
		if err != nil {
			yield(nil, err)
			return
		}
		yield(getADKResponse(resp), nil)
	}
}

func (a *ModelAdapter) resolveModel(ctx context.Context, req *model.LLMRequest) string {
	if ctxModel := modelFromContext(ctx); ctxModel != "" {
		return ctxModel
	}
	if req != nil && req.Model != "" {
		return req.Model
	}
	return a.defaultModel
}

func isRetryableError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var apiErr *responses.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 || apiErr.StatusCode == 429
	}
	return false
}
