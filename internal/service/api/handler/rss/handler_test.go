package rss

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasHTMLTags(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "순수 텍스트",
			content:  "안녕하세요.\n반갑습니다.",
			expected: false,
		},
		{
			name:     "br 태그 포함",
			content:  "안녕하세요.<br>반갑습니다.",
			expected: true,
		},
		{
			name:     "대소문자 혼용 태그",
			content:  "<P>테스트</P>",
			expected: true,
		},
		{
			name:     "img 태그 포함",
			content:  `<div><img src="test.jpg" alt="test"></div>`,
			expected: true,
		},
		{
			name:     "의미없는 단순 부등호",
			content:  "a < b 이고 c > d 이다.",
			expected: false,
		},
		{
			name:     "지원하지 않는 html 태그는 스킵",
			content:  "<span>그냥 텍스트</span>",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasHTMLTags.MatchString(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContentReplacerLogic(t *testing.T) {
	// handler.go 내에서 사용되는 치환 로직 검증
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "순수 텍스트 줄바꿈 변환",
			content:  "1번줄\n2번줄\r\n3번줄",
			expected: "1번줄<br>2번줄<br>3번줄",
		},
		{
			name:     "HTML 태그 포함 시 변환 안함",
			content:  "<div>\n1번줄\n</div>",
			expected: "<div>\n1번줄\n</div>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.content
			if !hasHTMLTags.MatchString(result) {
				result = contentReplacer.Replace(result)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}
