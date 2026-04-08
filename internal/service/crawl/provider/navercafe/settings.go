package navercafe

import (
	"strings"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/provider"
)

// crawlerSettings 네이버 카페 크롤러 구동을 위해 설정 파일의 "data" 항목에서 추가로 주입받는 전용 설정 정보를 담는 구조체입니다.
// ParseSettings 함수에 의해 설정 파일의 map 데이터로부터 자동으로 역직렬화됩니다.
type crawlerSettings struct {
	// ClubID 크롤링 대상 네이버 카페의 고유 식별자(Club ID)입니다. (필수)
	// 게시글 목록 조회, 게시글 링크 생성, 내부 API 호출 등 네이버 카페 관련 모든 요청에서
	// 특정 카페를 식별하기 위해 사용됩니다. 네이버 카페 URL에서 clubid 파라미터를 확인할 수 있습니다.
	ClubID string `json:"club_id"`

	// CrawlingDelayMinutes 게시글이 등록된 후 네이버 검색 색인에 반영되기까지 기다려야 하는 예상 지연 시간(분)입니다.
	//
	// 이 크롤러는 네이버 카페 게시글을 직접 방문하는 대신, 네이버 검색 색인을 통해 글 목록을 수집합니다.
	// 검색 색인에는 새 글이 즉시 반영되지 않으므로, 이 값만큼의 시간이 경과한 게시글부터 수집 대상으로 삼습니다.
	// 값이 0 이하이거나 생략된 경우 ApplyDefaults()에서 기본값(40분)이 자동으로 적용됩니다.
	CrawlingDelayMinutes int `json:"crawling_delay_minutes"`
}

// 컴파일 타임에 인터페이스 구현 여부를 검증합니다.
var _ provider.Defaulter = (*crawlerSettings)(nil)
var _ provider.Validator = (*crawlerSettings)(nil)

// ApplyDefaults 설정 파일에서 값이 제공되지 않았거나 유효하지 않은 선택적 필드에 기본값을 자동으로 주입합니다.
//
// 이 메서드는 Validate()보다 먼저 자동 호출되므로, Validate()에서는 기본값이 이미 채워진
// 안전한 상태를 전제하고 필수 항목 위주의 검증 로직만 작성하면 됩니다.
//
// 기본값:
//   - CrawlingDelayMinutes: 40분 (네이버 검색 색인 반영에 걸리는 일반적인 지연 시간)
func (s *crawlerSettings) ApplyDefaults() {
	if s.CrawlingDelayMinutes <= 0 {
		s.CrawlingDelayMinutes = 40
	}
}

// Validate 설정값의 유효성을 검증합니다.
//
// 이 메서드는 ApplyDefaults() 호출 이후 자동으로 실행됩니다.
// 치명적인 오류(필수 항목 누락 등)가 있을 경우 에러를 반환하여 크롤러 초기화를 중단시킵니다.
func (s *crawlerSettings) Validate() error {
	s.ClubID = strings.TrimSpace(s.ClubID)
	if s.ClubID == "" {
		return apperrors.New(apperrors.InvalidInput, "대상 카페의 고유 식별자(Club ID)는 필수 입력값입니다")
	}

	return nil
}
