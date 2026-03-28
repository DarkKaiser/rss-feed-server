package fetcher

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"
)

// BenchmarkDrainAndCloseBody 다양한 크기의 페이로드에 대한 drainAndCloseBody 성능 측정
func BenchmarkDrainAndCloseBody(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"Small_1KB", 1024},
		{"Medium_32KB", 32 * 1024},
		{"Large_64KB", 64 * 1024},       // maxDrainBytes와 동일
		{"ExtraLarge_1MB", 1024 * 1024}, // maxDrainBytes 초과
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			data := make([]byte, size.size)
			// 데이터 초기화는 벤치마크 시간에서 제외
			b.ResetTimer()

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// 매 반복마다 Reader 생성 (비용이 적으므로 포함)
				body := io.NopCloser(bytes.NewReader(data))
				drainAndCloseBody(body)
			}
		})
	}
}

// BenchmarkToCacheKey transportConfig를 transportCacheKey로 변환(정규화)하는 비용 측정
func BenchmarkToCacheKey(b *testing.B) {
	// 1. 모든 필드가 nil인 경우 (기본값 적용 비용)
	b.Run("AllNil", func(b *testing.B) {
		cfg := transportConfig{}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = cfg.ToCacheKey()
		}
	})

	// 2. 모든 필드가 설정된 경우 (값 복사 비용)
	b.Run("AllSet", func(b *testing.B) {
		cfg := transportConfig{
			proxyURL:              stringPtr("http://proxy.example.com:8080"),
			maxIdleConns:          intPtr(100),
			maxIdleConnsPerHost:   intPtr(10),
			maxConnsPerHost:       intPtr(100),
			tlsHandshakeTimeout:   durationPtr(10 * time.Second),
			responseHeaderTimeout: durationPtr(10 * time.Second),
			idleConnTimeout:       durationPtr(90 * time.Second),
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = cfg.ToCacheKey()
		}
	})
}

// BenchmarkNormalizeHelpers 개별 정규화 함수들의 성능 측정
func BenchmarkNormalizeHelpers(b *testing.B) {
	b.Run("MaxIdleConns", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = normalizeMaxIdleConns(-1)
			_ = normalizeMaxIdleConns(100)
		}
	})
	b.Run("TLSHandshakeTimeout", func(b *testing.B) {
		d := 10 * time.Second
		for i := 0; i < b.N; i++ {
			_ = normalizeTLSHandshakeTimeout(-1)
			_ = normalizeTLSHandshakeTimeout(d)
		}
	})
}

// BenchmarkTransportCreation Transport 생성(newTransport) 비용 측정
func BenchmarkTransportCreation(b *testing.B) {
	// 기본값 복제
	b.Run("Default_Clone", func(b *testing.B) {
		cfg := transportConfig{}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = newTransport(nil, cfg)
		}
	})

	// 프록시 파싱 포함
	b.Run("WithProxy_Parse", func(b *testing.B) {
		cfg := transportConfig{
			proxyURL: stringPtr("http://user:pass@proxy.example.com:8080"),
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = newTransport(nil, cfg)
		}
	})
}

// BenchmarkTransportCache Transport 캐시 조회 및 동시성 성능 측정
func BenchmarkTransportCache(b *testing.B) {
	// 테스트용 키 (transportConfig)
	cfg := transportConfig{
		proxyURL:            stringPtr("http://proxy.example.com:8080"),
		maxIdleConns:        intPtr(100),
		idleConnTimeout:     durationPtr(90 * time.Second),
		tlsHandshakeTimeout: durationPtr(10 * time.Second),
	}

	// 캐시에 미리 항목 추가
	_, _ = getSharedTransport(cfg)

	// 1. 순차적 조회 (Lock 경합 없음)
	b.Run("Sequential_Hit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = getSharedTransport(cfg)
		}
	})

	// 2. 동시 조회 (Read Lock 경합 및 Lazy LRU Update 테스트)
	b.Run("Concurrent_Hit", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _ = getSharedTransport(cfg)
			}
		})
	})

	// 3. 다양한 키로 캐시 조회 (Lock 경합, LRU 갱신, Eviction 시뮬레이션)
	b.Run("Concurrent_MixedKeys", func(b *testing.B) {
		// 10개의 서로 다른 설정 생성
		configs := make([]transportConfig, 10)
		for i := 0; i < 10; i++ {
			configs[i] = transportConfig{
				maxIdleConns: intPtr(i * 10), // 서로 다른 키 생성
			}
		}

		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				// Round-robin으로 키 선택하여 조회
				_, _ = getSharedTransport(configs[i%len(configs)])
				i++
			}
		})
	})
}

// BenchmarkConfigureTransportFromOptions 격리 모드 vs 공유 모드 성능 비교
func BenchmarkConfigureTransportFromOptions(b *testing.B) {
	// 공통 설정
	cfg := &HTTPFetcher{
		client:       &http.Client{},
		maxIdleConns: intPtr(100),
	}

	// 1. 공유 모드 (기본값, 캐시 사용)
	b.Run("SharedMode", func(b *testing.B) {
		// 캐시 사용 활성화
		cfg.disableTransportCaching = false
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = cfg.configureTransportFromOptions()
		}
	})

	// 2. 격리 모드 (매번 새로운 Transport 생성)
	b.Run("IsolatedMode", func(b *testing.B) {
		// 캐시 사용 비활성화
		cfg.disableTransportCaching = true
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = cfg.configureTransportFromOptions()
		}
	})
}
