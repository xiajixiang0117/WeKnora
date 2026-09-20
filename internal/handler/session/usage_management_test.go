package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type usageQuestionMessages struct {
	interfaces.MessageService
	pages [][]*types.Message
}

func (s *usageQuestionMessages) GetMessagesBySession(_ context.Context, _ string, page, _ int) ([]*types.Message, error) {
	return s.pages[page-1], nil
}

func TestSessionUsageQuestionsMatchRequestsAcrossPages(t *testing.T) {
	first := make([]*types.Message, sessionUsageMessagePageSize)
	first[0] = &types.Message{Role: "assistant", RequestID: "second"}
	first[1] = &types.Message{Role: "user", RequestID: "first", Content: "第一问\n完整正文"}
	first[2] = &types.Message{Role: "assistant", RequestID: "first"}
	first[3] = &types.Message{Role: "user", Content: "没有请求 ID 的问题"}
	first[4] = &types.Message{Role: "assistant"}
	first[5] = &types.Message{Role: "assistant", RequestID: "missing"}
	h := &Handler{messageService: &usageQuestionMessages{pages: [][]*types.Message{
		first,
		{{Role: "user", RequestID: "second", Content: "第二问"}},
	}}}
	summary, details, err := h.buildSessionUsage(context.Background(), &types.SessionListItem{}, nil, nil, true)
	require.NoError(t, err)
	require.Equal(t, 4, summary.AnswerCount)
	require.Len(t, details, 4)
	require.Equal(t, "第二问", details[0].Question)
	require.Equal(t, "第一问\n完整正文", details[1].Question)
	require.Empty(t, details[2].Question)
	require.Empty(t, details[3].Question)
}

func TestSessionUsageChannelClassifiesEverySupportedSessionOrigin(t *testing.T) {
	tests := []struct {
		name    string
		session types.SessionListItem
		want    string
		wantID  string
	}{
		{
			name:    "web",
			session: types.SessionListItem{Session: types.Session{UserID: "alice"}},
			want:    "web",
		},
		{
			name: "api",
			session: types.SessionListItem{Session: types.Session{
				UserID: types.SessionOwnerAPITenantKeyPrefix + "1:3",
			}},
			want: "api",
		},
		{
			name: "web embed",
			session: types.SessionListItem{Session: types.Session{
				Description: types.EmbedSessionMarkerPrefix + "website-support",
			}},
			want:   "embed",
			wantID: "website-support",
		},
		{
			name: "wecom",
			session: types.SessionListItem{
				Session:     types.Session{},
				IMPlatform:  "wecom",
				IMChannelID: "im-channel-1",
			},
			want:   "wecom",
			wantID: "im-channel-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, sessionUsageChannel(&tt.session))
			require.Equal(t, tt.wantID, sessionUsageChannelID(&tt.session))
		})
	}
}

func TestNormalizedUsageSynthesizesOnlyMissingTotal(t *testing.T) {
	missingTotal := normalizedUsage(types.TokenUsage{PromptTokens: 120, CompletionTokens: 34})
	require.Equal(t, 154, missingTotal.TotalTokens)

	reportedTotal := normalizedUsage(types.TokenUsage{PromptTokens: 120, CompletionTokens: 34, TotalTokens: 999})
	require.Equal(t, 999, reportedTotal.TotalTokens)
}

func TestSessionUsageModelNamePrefersDisplayName(t *testing.T) {
	require.Equal(t, "Customer Support", sessionUsageModelName(&types.Model{
		Name:        "gpt-4.1",
		DisplayName: " Customer Support ",
	}))
	require.Equal(t, "gpt-4.1", sessionUsageModelName(&types.Model{Name: " gpt-4.1 "}))
	require.Empty(t, sessionUsageModelName(nil))
}
