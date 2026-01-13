package mattermost_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/user/mattermost-tools/pkg/mattermost"
	"github.com/user/mattermost-tools/pkg/mattermost/mocks"
)

func TestWebhook_Post_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "https://mattermost.example.com/hooks/xxx", req.URL.String())
			require.Equal(t, "application/json", req.Header.Get("Content-Type"))

			body, _ := io.ReadAll(req.Body)
			require.Contains(t, string(body), "Test message")

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		})

	webhook := mattermost.NewWebhookWithHTTP("https://mattermost.example.com/hooks/xxx", mockHTTP)
	err := webhook.Post(context.Background(), "Test message")

	require.NoError(t, err)
}

func TestWebhook_Post_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHTTP := mocks.NewMockHTTPDoer(ctrl)
	mockHTTP.EXPECT().
		Do(gomock.Any()).
		Return(&http.Response{
			StatusCode: 500,
			Body:       io.NopCloser(strings.NewReader("internal error")),
		}, nil)

	webhook := mattermost.NewWebhookWithHTTP("https://mattermost.example.com/hooks/xxx", mockHTTP)
	err := webhook.Post(context.Background(), "Test message")

	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}
