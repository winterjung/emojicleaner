package main

import (
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_extractEmojisFromText(t *testing.T) {
	cases := []struct {
		name     string
		given    string
		expected []string
	}{
		{
			name:     "길어도 됨",
			given:    `:alphabet-yellow-w: :icon-colored-category-account-book-online-shopping-large1:`,
			expected: []string{"alphabet-yellow-w", "icon-colored-category-account-book-online-shopping-large1"},
		},
		{
			name: "list와 헷갈리지 말기",
			given: `- 첫번째: 이건 이거고
- 두번째: 이건 :tada:고
- 세번째: 이건 https://link 링크다`,
			expected: []string{"tada"},
		},
		{
			name: "3개가 나와야함",
			given: `:google-docs:*<https://link|구글 독스>* :tada: <!subteam^S71TDG26A|@그룹태그>
	어쩌구저쩌구 (:green_salad: 테크샐러드)`,
			expected: []string{"google-docs", "tada", "green_salad"},
		},
		{
			name:     "없어야함",
			given:    "이모지가 아무것도 없는 텍스트",
			expected: nil,
		},
		{
			name:     "연달아 이모지",
			given:    ":smile::heart::tada:",
			expected: []string{"smile", "heart", "tada"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := extractEmojisFromText(tc.given)

			assert.Equal(t, tc.expected, got)
		})
	}
}

func Test_countRawEmojisFromMessage(t *testing.T) {
	got := countRawEmojisFromMessage(slack.Message{
		Msg: slack.Msg{
			Text: ":+1: :+1: :1+: :-1:",
			Reactions: []slack.ItemReaction{
				{
					Name:  "+1",
					Count: 10,
				},
				{
					Name:  "+1::skin-tone-3",
					Count: 5,
				},
			},
		},
	})
	assert.Equal(t, map[string]int{
		"+1": 17,
		"1+": 1,
		"-1": 1,
	}, got)
}

func Test_removeSkinTone(t *testing.T) {
	assert.Equal(t, "+1", removeSkinTone("+1"))
	assert.Equal(t, "+1", removeSkinTone("+1::skin-tone-3"))
}
