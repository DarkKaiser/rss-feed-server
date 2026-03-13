package config

import (
	"os"
	"testing"

	v10 "github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 헬퍼
// ─────────────────────────────────────────────────────────────────────────────

// validProvider는 반복적으로 사용되는 유효한 ProviderConfig를 생성합니다.
func validProvider(id, site string) *ProviderConfig {
	return &ProviderConfig{
		ID:   id,
		Site: site,
		Config: &ProviderDetailConfig{
			ID:   id + "_cfg",
			Name: id + "_name",
			URL:  "http://example.com/" + id,
		},
		Scheduler: SchedulerConfig{TimeSpec: "@every 5m"},
	}
}

// validNaverCafeProvider는 유효한 네이버 카페 ProviderConfig를 생성합니다.
func validNaverCafeProvider(id, clubID string) *ProviderConfig {
	p := validProvider(id, string(ProviderSiteNaverCafe))
	p.Config.Data = map[string]any{"club_id": clubID}
	return p
}

// newTestValidator는 테스트용 validator 인스턴스를 생성합니다.
func newTestValidator() *v10.Validate {
	return newValidator()
}

// ─────────────────────────────────────────────────────────────────────────────
// AppConfig
// ─────────────────────────────────────────────────────────────────────────────

func TestAppConfig_Validate(t *testing.T) {
	v := newTestValidator()

	t.Run("유효한 최소 설정", func(t *testing.T) {
		cfg := AppConfig{
			RssFeed: RssFeedConfig{
				MaxItemCount: 10,
				Providers:    []*ProviderConfig{validProvider("p1", string(ProviderSiteYeosuCityHall))},
			},
			WS: WSConfig{ListenPort: 8080},
		}
		assert.NoError(t, cfg.validate(v))
	})

	t.Run("RssFeed 오류가 상위로 전파됨", func(t *testing.T) {
		cfg := AppConfig{
			RssFeed: RssFeedConfig{MaxItemCount: 0}, // 위반
			WS:      WSConfig{ListenPort: 8080},
		}
		err := cfg.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max_item_count")
	})

	t.Run("WS 오류가 상위로 전파됨", func(t *testing.T) {
		cfg := AppConfig{
			RssFeed: RssFeedConfig{
				MaxItemCount: 10,
				Providers:    []*ProviderConfig{validProvider("p1", string(ProviderSiteYeosuCityHall))},
			},
			WS: WSConfig{ListenPort: 0}, // 위반
		}
		err := cfg.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listen_port")
	})

	t.Run("NotifyAPI 오류가 상위로 전파됨", func(t *testing.T) {
		// NotifyAPIConfig 자체에 required 태그가 없으므로 커스텀 Validator를 주입하여 강제로 에러를 만듭니다.
		customV := v10.New()
		customV.RegisterStructValidation(func(sl v10.StructLevel) {
			sl.ReportError(sl.Current().Interface(), "URL", "url", "force_error", "")
		}, NotifyAPIConfig{})

		cfg := AppConfig{
			RssFeed: RssFeedConfig{
				MaxItemCount: 10,
				Providers:    []*ProviderConfig{validProvider("p1", string(ProviderSiteYeosuCityHall))},
			},
			WS: WSConfig{ListenPort: 8080},
		}
		err := cfg.validate(customV)
		require.Error(t, err)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// AppConfig.lint
// ─────────────────────────────────────────────────────────────────────────────

func TestAppConfig_Lint(t *testing.T) {
	t.Run("시스템 예약 포트 사용 시 경고 반환", func(t *testing.T) {
		cfg := newDefaultConfig()
		cfg.WS.ListenPort = 80
		warnings := cfg.lint()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "시스템 예약 포트(1-1023)를 사용하도록 설정되었습니다")
	})

	t.Run("비예약 포트 사용 시 경고 없음", func(t *testing.T) {
		cfg := newDefaultConfig()
		cfg.WS.ListenPort = 8080
		assert.Empty(t, cfg.lint())
	})

	t.Run("포트 경계값(1024) 사용 시 경고 없음", func(t *testing.T) {
		cfg := newDefaultConfig()
		cfg.WS.ListenPort = 1024
		assert.Empty(t, cfg.lint())
	})

	t.Run("포트 경계값(1023) 사용 시 경고 반환", func(t *testing.T) {
		cfg := newDefaultConfig()
		cfg.WS.ListenPort = 1023
		assert.Len(t, cfg.lint(), 1)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// RssFeedConfig
// ─────────────────────────────────────────────────────────────────────────────

func TestRssFeedConfig_Validate(t *testing.T) {
	v := newTestValidator()

	t.Run("유효한 설정", func(t *testing.T) {
		cfg := RssFeedConfig{
			MaxItemCount: 10,
			Providers:    []*ProviderConfig{validProvider("p1", string(ProviderSiteYeosuCityHall))},
		}
		assert.NoError(t, cfg.validate(v))
	})

	t.Run("MaxItemCount가 0이면 에러", func(t *testing.T) {
		cfg := RssFeedConfig{MaxItemCount: 0}
		err := cfg.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "RSS 피드 최대 수집 개수(max_item_count)는 0보다 커야 합니다")
	})

	t.Run("Provider ID 중복이면 에러", func(t *testing.T) {
		cfg := RssFeedConfig{
			MaxItemCount: 10,
			Providers: []*ProviderConfig{
				validProvider("dup_id", string(ProviderSiteYeosuCityHall)),
				validProvider("dup_id", string(ProviderSiteYeosuCityHall)),
			},
		}
		err := cfg.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "RSS 피드 설정 내에 중복된 RSS 피드 공급자(Provider) ID가 존재합니다")
	})

	t.Run("Provider 하위 에러가 상위로 전파됨", func(t *testing.T) {
		cfg := RssFeedConfig{
			MaxItemCount: 10,
			Providers:    []*ProviderConfig{{ID: "", Site: "", Config: nil}},
		}
		err := cfg.validate(v)
		require.Error(t, err)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ProviderConfig
// ─────────────────────────────────────────────────────────────────────────────

func TestProviderConfig_Validate(t *testing.T) {
	v := newTestValidator()
	seen := func() map[string]string { return make(map[string]string) }

	t.Run("ID 누락 시 에러", func(t *testing.T) {
		p := validProvider("", string(ProviderSiteYeosuCityHall))
		p.ID = ""
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id (조건: required)")
	})

	t.Run("Site 누락 시 에러", func(t *testing.T) {
		p := validProvider("p1", "")
		p.Site = ""
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "site (조건: required)")
	})

	t.Run("Config 누락 시 에러", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteYeosuCityHall))
		p.Config = nil
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "config (조건: required)")
	})

	t.Run("지원하지 않는 Site 설정 시 에러", func(t *testing.T) {
		p := validProvider("p1", "UnknownSite")
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "지원하지 않는 사이트('UnknownSite')가 설정되었습니다")
	})

	t.Run("Scheduler TimeSpec 누락 시 에러", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteYeosuCityHall))
		p.Scheduler.TimeSpec = ""
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "time_spec (조건: required)")
	})

	t.Run("Scheduler TimeSpec이 유효하지 않으면 에러", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteYeosuCityHall))
		p.Scheduler.TimeSpec = "not-a-cron"
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "time_spec 설정이 유효하지 않습니다")
	})
}

func TestProviderConfig_Validate_NaverCafe(t *testing.T) {
	v := newTestValidator()
	seen := func() map[string]string { return make(map[string]string) }

	t.Run("유효한 네이버 카페 설정", func(t *testing.T) {
		p := validNaverCafeProvider("p1", "123456")
		assert.NoError(t, p.validate(v, seen()))
	})

	t.Run("club_id 미설정 시 에러", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteNaverCafe))
		p.Config.Data = nil
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "club_id가 입력되지 않았거나 문자열 타입이 아닙니다")
	})

	t.Run("club_id가 공백 문자열이면 에러", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteNaverCafe))
		p.Config.Data = map[string]any{"club_id": "   "}
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "club_id가 입력되지 않았거나 문자열 타입이 아닙니다")
	})

	t.Run("club_id가 문자열 타입이 아니면 에러", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteNaverCafe))
		p.Config.Data = map[string]any{"club_id": 12345} // int 타입
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "club_id가 입력되지 않았거나 문자열 타입이 아닙니다")
	})

	t.Run("club_id 중복 시 에러", func(t *testing.T) {
		seenClubIDs := seen()
		p1 := validNaverCafeProvider("p1", "same_club")
		require.NoError(t, p1.validate(v, seenClubIDs))

		p2 := validNaverCafeProvider("p2", "same_club")
		err := p2.validate(v, seenClubIDs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "club_id('same_club')가 중복되었습니다")
		assert.Contains(t, err.Error(), "p1")
		assert.Contains(t, err.Error(), "p2")
	})

	t.Run("서로 다른 club_id는 중복 에러 없음", func(t *testing.T) {
		seenClubIDs := seen()
		p1 := validNaverCafeProvider("p1", "club_a")
		p2 := validNaverCafeProvider("p2", "club_b")
		require.NoError(t, p1.validate(v, seenClubIDs))
		assert.NoError(t, p2.validate(v, seenClubIDs))
	})

	t.Run("ProviderDetailConfig 하위 오류가 전파됨 (게시판 ID 누락)", func(t *testing.T) {
		p := validNaverCafeProvider("p1", "123456")
		p.Config.Boards = []*BoardConfig{{Name: "공지"}} // ID 누락
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id (조건: required)")
	})
}

func TestProviderConfig_Validate_YeosuCityHall(t *testing.T) {
	v := newTestValidator()
	seen := func() map[string]string { return make(map[string]string) }

	t.Run("유효한 여수시청 설정", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteYeosuCityHall))
		assert.NoError(t, p.validate(v, seen()))
	})

	t.Run("ProviderDetailConfig URL 누락 시 에러", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteYeosuCityHall))
		p.Config.URL = ""
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url (조건: required)")
	})
}

func TestProviderConfig_Validate_SsangbongElementarySchool(t *testing.T) {
	v := newTestValidator()
	seen := func() map[string]string { return make(map[string]string) }

	t.Run("유효한 쌍봉초등학교 설정", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteSsangbongElementarySchool))
		assert.NoError(t, p.validate(v, seen()))
	})

	t.Run("ProviderDetailConfig URL 누락 시 에러", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteSsangbongElementarySchool))
		p.Config.URL = ""
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url (조건: required)")
	})

	t.Run("ProviderDetailConfig 하위 오류가 전파됨 (게시판 ID 누락)", func(t *testing.T) {
		p := validProvider("p1", string(ProviderSiteSsangbongElementarySchool))
		p.Config.Boards = []*BoardConfig{{Name: "공지"}} // ID 누락
		err := p.validate(v, seen())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id (조건: required)")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ProviderDetailConfig
// ─────────────────────────────────────────────────────────────────────────────

func TestProviderDetailConfig_Validate(t *testing.T) {
	v := newTestValidator()

	t.Run("유효한 설정", func(t *testing.T) {
		cfg := &ProviderDetailConfig{
			ID:   "cfg1",
			Name: "공급자1",
			URL:  "http://example.com/path",
		}
		assert.NoError(t, cfg.validate(v, "테스트"))
	})

	t.Run("ID 누락 시 에러", func(t *testing.T) {
		cfg := &ProviderDetailConfig{Name: "공급자1", URL: "http://example.com"}
		err := cfg.validate(v, "테스트")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id (조건: required)")
	})

	t.Run("Name 누락 시 에러", func(t *testing.T) {
		cfg := &ProviderDetailConfig{ID: "cfg1", URL: "http://example.com"}
		err := cfg.validate(v, "테스트")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name (조건: required)")
	})

	t.Run("URL 누락 시 에러", func(t *testing.T) {
		cfg := &ProviderDetailConfig{ID: "cfg1", Name: "공급자1"}
		err := cfg.validate(v, "테스트")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url (조건: required)")
	})

	t.Run("URL 끝의 슬래시가 제거됨", func(t *testing.T) {
		cfg := &ProviderDetailConfig{ID: "cfg1", Name: "공급자1", URL: "http://example.com/path/"}
		require.NoError(t, cfg.validate(v, "테스트"))
		assert.Equal(t, "http://example.com/path", cfg.URL)
	})

	t.Run("URL 끝에 슬래시 없으면 그대로 유지", func(t *testing.T) {
		cfg := &ProviderDetailConfig{ID: "cfg1", Name: "공급자1", URL: "http://example.com/path"}
		require.NoError(t, cfg.validate(v, "테스트"))
		assert.Equal(t, "http://example.com/path", cfg.URL)
	})

	t.Run("Board ID 중복 시 에러", func(t *testing.T) {
		cfg := &ProviderDetailConfig{
			ID:   "cfg1",
			Name: "공급자1",
			URL:  "http://example.com",
			Boards: []*BoardConfig{
				{ID: "dup", Name: "게시판A"},
				{ID: "dup", Name: "게시판B"},
			},
		}
		err := cfg.validate(v, "테스트")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "중복된 게시판(Board) ID가 존재합니다")
	})

	t.Run("Board 하위 에러가 전파됨 (ID 누락)", func(t *testing.T) {
		cfg := &ProviderDetailConfig{
			ID:     "cfg1",
			Name:   "공급자1",
			URL:    "http://example.com",
			Boards: []*BoardConfig{{Name: "공지"}}, // ID 누락
		}
		err := cfg.validate(v, "테스트")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id (조건: required)")
	})
}

func TestProviderDetailConfig_HasBoard(t *testing.T) {
	cfg := &ProviderDetailConfig{
		Boards: []*BoardConfig{
			{ID: "board1", Name: "게시판1"},
			{ID: "board2", Name: "게시판2"},
		},
	}

	t.Run("존재하는 ID로 조회 시 true", func(t *testing.T) {
		assert.True(t, cfg.HasBoard("board1"))
		assert.True(t, cfg.HasBoard("board2"))
	})

	t.Run("존재하지 않는 ID로 조회 시 false", func(t *testing.T) {
		assert.False(t, cfg.HasBoard("board3"))
	})

	t.Run("빈 문자열로 조회 시 false", func(t *testing.T) {
		assert.False(t, cfg.HasBoard(""))
	})

	t.Run("공백 포함 ID로 조회 시 false (정확히 일치해야 함)", func(t *testing.T) {
		assert.False(t, cfg.HasBoard("board1 "))
	})

	t.Run("Boards가 비어있으면 항상 false", func(t *testing.T) {
		empty := &ProviderDetailConfig{}
		assert.False(t, empty.HasBoard("board1"))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// BoardConfig
// ─────────────────────────────────────────────────────────────────────────────

func TestBoardConfig_Validate(t *testing.T) {
	v := newTestValidator()

	t.Run("유효한 설정", func(t *testing.T) {
		board := &BoardConfig{ID: "b1", Name: "공지사항"}
		assert.NoError(t, board.validate(v, "prov_id", "테스트공급자"))
	})

	t.Run("ID 누락 시 에러", func(t *testing.T) {
		board := &BoardConfig{Name: "공지사항"}
		err := board.validate(v, "prov_id", "테스트공급자")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id (조건: required)")
	})

	t.Run("Name 누락 시 에러", func(t *testing.T) {
		board := &BoardConfig{ID: "b1"}
		err := board.validate(v, "prov_id", "테스트공급자")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name (조건: required)")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// WSConfig
// ─────────────────────────────────────────────────────────────────────────────

func TestWSConfig_Validate(t *testing.T) {
	v := newTestValidator()

	t.Run("유효한 포트 설정", func(t *testing.T) {
		ws := &WSConfig{ListenPort: 8080}
		assert.NoError(t, ws.validate(v))
	})

	t.Run("포트 0 설정 시 에러", func(t *testing.T) {
		ws := &WSConfig{ListenPort: 0}
		err := ws.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "웹 서비스 포트(listen_port)는 1에서 65535 사이의 값이어야 합니다")
	})

	t.Run("포트 65536 초과 시 에러", func(t *testing.T) {
		ws := &WSConfig{ListenPort: 65536}
		err := ws.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "웹 서비스 포트(listen_port)는 1에서 65535 사이의 값이어야 합니다")
	})

	t.Run("포트 경계값(1) 유효", func(t *testing.T) {
		ws := &WSConfig{ListenPort: 1}
		assert.NoError(t, ws.validate(v))
	})

	t.Run("포트 경계값(65535) 유효", func(t *testing.T) {
		ws := &WSConfig{ListenPort: 65535}
		assert.NoError(t, ws.validate(v))
	})
}

func TestWSConfig_Validate_TLS(t *testing.T) {
	v := newTestValidator()

	// 테스트용 임시 파일을 생성하고 경로를 반환합니다.
	newTempFile := func(t *testing.T) string {
		t.Helper()
		f, err := os.CreateTemp("", "tls_test_*")
		require.NoError(t, err)
		t.Cleanup(func() { os.Remove(f.Name()) })
		f.Close()
		return f.Name()
	}

	t.Run("TLS 비활성화 시 cert/key 파일 없어도 유효", func(t *testing.T) {
		ws := &WSConfig{TLSServer: false, ListenPort: 8080}
		assert.NoError(t, ws.validate(v))
	})

	t.Run("TLS 활성화 시 cert 파일 누락이면 에러", func(t *testing.T) {
		ws := &WSConfig{TLSServer: true, ListenPort: 8443}
		err := ws.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TLS 서버 활성화 시 TLS 인증서 파일 경로(tls_cert_file)는 필수입니다")
	})

	t.Run("TLS 활성화 시 cert 파일은 있으나 key 파일 누락이면 에러", func(t *testing.T) {
		ws := &WSConfig{
			TLSServer:   true,
			TLSCertFile: newTempFile(t),
			TLSKeyFile:  "",
			ListenPort:  8443,
		}
		err := ws.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TLS 서버 활성화 시 TLS 키 파일 경로(tls_key_file)는 필수입니다")
	})

	t.Run("TLS 활성화 시 존재하지 않는 cert 파일 경로면 에러", func(t *testing.T) {
		ws := &WSConfig{
			TLSServer:   true,
			TLSCertFile: "non_existent.crt",
			TLSKeyFile:  newTempFile(t),
			ListenPort:  8443,
		}
		err := ws.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "지정된 TLS 인증서 파일(tls_cert_file)을 찾을 수 없습니다: 'non_existent.crt'")
	})

	t.Run("TLS 활성화 시 존재하지 않는 key 파일 경로면 에러", func(t *testing.T) {
		ws := &WSConfig{
			TLSServer:   true,
			TLSCertFile: newTempFile(t),
			TLSKeyFile:  "non_existent.key",
			ListenPort:  8443,
		}
		err := ws.validate(v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "지정된 TLS 키 파일(tls_key_file)을 찾을 수 없습니다: 'non_existent.key'")
	})

	t.Run("TLS 활성화 시 유효한 cert/key 파일이면 성공", func(t *testing.T) {
		ws := &WSConfig{
			TLSServer:   true,
			TLSCertFile: newTempFile(t),
			TLSKeyFile:  newTempFile(t),
			ListenPort:  8443,
		}
		assert.NoError(t, ws.validate(v))
	})
}

func TestWSConfig_Lint(t *testing.T) {
	t.Run("포트 1 사용 시 경고 반환", func(t *testing.T) {
		ws := &WSConfig{ListenPort: 1}
		warnings := ws.lint()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "시스템 예약 포트")
	})

	t.Run("포트 1023 사용 시 경고 반환", func(t *testing.T) {
		ws := &WSConfig{ListenPort: 1023}
		assert.Len(t, ws.lint(), 1)
	})

	t.Run("포트 1024 사용 시 경고 없음", func(t *testing.T) {
		ws := &WSConfig{ListenPort: 1024}
		assert.Empty(t, ws.lint())
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// NotifyAPIConfig
// ─────────────────────────────────────────────────────────────────────────────

func TestNotifyAPIConfig_Validate(t *testing.T) {
	v := newTestValidator()

	t.Run("필드 모두 비어있어도 유효 (required 태그 없음)", func(t *testing.T) {
		cfg := &NotifyAPIConfig{}
		assert.NoError(t, cfg.validate(v))
	})

	t.Run("필드 채워진 경우 유효", func(t *testing.T) {
		cfg := &NotifyAPIConfig{
			URL:           "http://notify.example.com/api",
			AppKey:        "test_app_key",
			ApplicationID: "test_app_id",
		}
		assert.NoError(t, cfg.validate(v))
	})
}
