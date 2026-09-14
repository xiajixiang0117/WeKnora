package session

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

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
