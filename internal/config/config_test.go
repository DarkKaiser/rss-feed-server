package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 헬퍼
// ─────────────────────────────────────────────────────────────────────────────

// minimalValidConfigJSON은 유효성 검사를 통과하는 최소한의 JSON 설정입니다.
const minimalValidConfigJSON = `{
	"rss_feed": {
		"providers": [
			{
				"id":   "p1",
				"site": "YeosuCityHall",
				"config": {
					"id":   "cfg1",
					"name": "테스트공급자",
					"url":  "http://example.com"
				},
				"scheduler": { "time_spec": "@every 5m" }
			}
		]
	},
	"ws": { "listen_port": 8080 }
}`

// writeTempConfig는 임시 JSON 설정 파일을 생성하고, 테스트 종료 시 자동으로 삭제합니다.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "config_test_*.json")
	require.NoError(t, err)
	t.Cleanup(func() { os.Remove(f.Name()) })
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// ─────────────────────────────────────────────────────────────────────────────
// newDefaultConfig
// ─────────────────────────────────────────────────────────────────────────────

func TestNewDefaultConfig(t *testing.T) {
	cfg := newDefaultConfig()

	t.Run("Debug 기본값은 false", func(t *testing.T) {
		assert.False(t, cfg.Debug)
	})

	t.Run("MaxItemCount 기본값 확인", func(t *testing.T) {
		assert.Equal(t, uint(DefaultMaxItemCount), cfg.RSSFeed.MaxItemCount)
	})

	t.Run("ListenPort 기본값 확인", func(t *testing.T) {
		assert.Equal(t, DefaultListenPort, cfg.WS.ListenPort)
	})

	t.Run("Providers 기본값은 nil (빈 슬라이스)", func(t *testing.T) {
		assert.Empty(t, cfg.RSSFeed.Providers)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Load
// ─────────────────────────────────────────────────────────────────────────────

func TestLoad_DefaultFileNotFound(t *testing.T) {
	// 프로젝트 루트가 아닌 임시 디렉터리에서 Load()를 호출하여
	// DefaultFilename(rss-feed-server.json)이 없는 상황을 재현합니다.
	// os.Chdir 대신 임시 디렉터리를 작업 디렉터리로 전환합니다.
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { os.Chdir(origDir) })

	tmpDir, err := os.MkdirTemp("", "load_test_*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	require.NoError(t, os.Chdir(tmpDir))

	cfg, warnings, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, warnings)
	assert.Contains(t, err.Error(), "설정 파일을 찾을 수 없습니다")
}

// ─────────────────────────────────────────────────────────────────────────────
// LoadWithFile
// ─────────────────────────────────────────────────────────────────────────────

func TestLoadWithFile_FileNotFound(t *testing.T) {
	cfg, warnings, err := LoadWithFile("non_existent_file.json")
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, warnings)
	assert.Contains(t, err.Error(), "설정 파일을 찾을 수 없습니다")
}

func TestLoadWithFile_InvalidJSON(t *testing.T) {
	path := writeTempConfig(t, `{ invalid json `)

	cfg, warnings, err := LoadWithFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, warnings)
	assert.Contains(t, err.Error(), "설정 파일 로드 중 오류가 발생하였습니다")
}

func TestLoadWithFile_DirectoryAsFile(t *testing.T) {
	// 디렉터리를 파일처럼 읽으려 하면 파서가 에러를 반환합니다.
	tmpDir, err := os.MkdirTemp("", "config_dir_test_*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	cfg, warnings, err := LoadWithFile(tmpDir)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, warnings)
	assert.Contains(t, err.Error(), "설정 파일 로드 중 오류가 발생하였습니다")
}

func TestLoadWithFile_UnmarshalError(t *testing.T) {
	// listen_port에 문자열을 설정하면 mapstructure가 WeaklyTypedInput=true임에도
	// 숫자로 변환하지 못해 에러를 반환합니다.
	content := `{ "ws": { "listen_port": "not_a_number" } }`
	path := writeTempConfig(t, content)

	cfg, warnings, err := LoadWithFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, warnings)
	assert.Contains(t, err.Error(), "데이터 타입(숫자/문자 등)을 확인해주세요")
}

func TestLoadWithFile_ValidationError(t *testing.T) {
	// 유효성 검사를 통과하지 못하는 설정: MaxItemCount = 0
	content := `{
		"rss_feed": { "max_item_count": 0 },
		"ws": { "listen_port": 8080 }
	}`
	path := writeTempConfig(t, content)

	cfg, warnings, err := LoadWithFile(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, warnings)
	assert.Contains(t, err.Error(), "유효성 검증에 실패하였습니다")
}

func TestLoadWithFile_Success_MinimalConfig(t *testing.T) {
	path := writeTempConfig(t, minimalValidConfigJSON)

	cfg, warnings, err := LoadWithFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	// listen_port가 8080으로 설정되어 있으므로 lint 경고 없음
	assert.Empty(t, warnings)
}

func TestLoadWithFile_Success_DefaultsApplied(t *testing.T) {
	// rss_feed.max_item_count를 생략하면 기본값(DefaultMaxItemCount)이 적용되는지 확인합니다.
	path := writeTempConfig(t, minimalValidConfigJSON)

	cfg, _, err := LoadWithFile(path)
	require.NoError(t, err)
	assert.Equal(t, uint(DefaultMaxItemCount), cfg.RSSFeed.MaxItemCount)
}

func TestLoadWithFile_Success_LintWarning(t *testing.T) {
	// listen_port를 예약 포트(80)로 설정하면 lint 경고가 반환되어야 합니다.
	content := strings.ReplaceAll(minimalValidConfigJSON, `"listen_port": 8080`, `"listen_port": 80`)
	path := writeTempConfig(t, content)

	cfg, warnings, err := LoadWithFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "시스템 예약 포트(1-1023)를 사용하도록 설정되었습니다")
}

func TestLoadWithFile_Success_URLTrailingSlashTrimmed(t *testing.T) {
	// URL 끝의 슬래시가 자동으로 제거되었는지 확인합니다.
	content := strings.ReplaceAll(minimalValidConfigJSON, `"url":  "http://example.com"`, `"url": "http://example.com/"`)
	path := writeTempConfig(t, content)

	cfg, _, err := LoadWithFile(path)
	require.NoError(t, err)
	assert.Equal(t, "http://example.com", cfg.RSSFeed.Providers[0].Config.URL)
}

func TestLoadWithFile_EnvOverride(t *testing.T) {
	path := writeTempConfig(t, minimalValidConfigJSON)

	t.Setenv("RSSFEED_DEBUG", "true")
	t.Setenv("RSSFEED_WS__LISTEN_PORT", "9090")

	cfg, _, err := LoadWithFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	t.Run("debug 환경변수가 설정 파일 값을 덮어씀", func(t *testing.T) {
		assert.True(t, cfg.Debug)
	})

	t.Run("listen_port 환경변수가 설정 파일 값을 덮어씀", func(t *testing.T) {
		assert.Equal(t, 9090, cfg.WS.ListenPort)
	})
}

func TestLoadWithFile_EnvOverride_NotApplied(t *testing.T) {
	// RSSFEED_ 접두사가 없는 환경변수는 무시되어야 합니다.
	path := writeTempConfig(t, minimalValidConfigJSON)
	t.Setenv("WS__LISTEN_PORT", "7777")

	cfg, _, err := LoadWithFile(path)
	require.NoError(t, err)
	// 환경변수가 무시되었으므로 설정 파일의 8080이 유지됩니다.
	assert.Equal(t, 8080, cfg.WS.ListenPort)
}

func TestLoadWithFile_AbsolutePath(t *testing.T) {
	// LoadWithFile이 절대 경로를 사용할 수 있는지 확인합니다.
	path := writeTempConfig(t, minimalValidConfigJSON)
	absPath, err := filepath.Abs(path)
	require.NoError(t, err)

	cfg, _, err := LoadWithFile(absPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

// ─────────────────────────────────────────────────────────────────────────────
// normalizeEnvKey
// ─────────────────────────────────────────────────────────────────────────────

func TestNormalizeEnvKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "정상적인 환경변수 (이중 언더스코어 → 점)",
			input:    "RSSFEED_WS__LISTEN_PORT",
			expected: "ws.listen_port",
		},
		{
			name:     "소문자 변환 확인",
			input:    "RSSFEED_rssfeed__MAXITEMCOUNT",
			expected: "rssfeed.maxitemcount",
		},
		{
			name:     "접두사가 없는 경우 (접두사 제거 안됨, 소문자 변환만)",
			input:    "OTHER_WS__PORT",
			expected: "other_ws.port",
		},
		{
			name:     "이중 언더스코어가 없는 경우",
			input:    "RSSFEED_WSPORT",
			expected: "wsport",
		},
		{
			name:     "접두사만 있는 경우",
			input:    "RSSFEED_",
			expected: "",
		},
		{
			name:     "3단계 이상 계층 구조",
			input:    "RSSFEED_RSS_FEED__MAX__ITEM",
			expected: "rss_feed.max.item",
		},
		{
			name:     "연속된 이중 언더스코어",
			input:    "RSSFEED_A____B",
			expected: fmt.Sprintf("a%sb", strings.Repeat(".", 2)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := normalizeEnvKey(tc.input)
			assert.Equal(t, tc.expected, actual)
		})
	}
}
