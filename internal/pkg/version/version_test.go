package version

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Documentation Examples (GoDoc)
// =============================================================================

func Example() {
	// 1. 빌드 정보 조회
	// 실제 환경에서 버전 정보는 링커 플래그(-ldflags)를 통해 주입됩니다.
	// 따라서 별도의 설정 없이 Get() 함수를 호출하여 안전하게 정보를 조회할 수 있습니다.
	current := Get()

	// 예시 출력을 위한 가상 데이터 설정 (실제 코드에서는 불필요)
	// 이 부분은 문서화된 예제 실행을 위해 임의로 값을 보여주는 것입니다.
	if current.Version == "unknown" {
		fmt.Printf("App Version: %s\n", current.Version)
	} else {
		// 테스트 환경에 따라 버전이 다를 수 있으므로 포맷만 확인
		fmt.Printf("App Version: <checked>\n")
	}

	// Output:
	// App Version: unknown
}

// =============================================================================
// Unit Tests
// =============================================================================

// TestInfo_String_Formatting은 String() 메서드의 출력 포맷을 검증합니다.
// 특히 SemVer 규격에 따른 메타데이터 표기(+dirty)를 중점적으로 확인합니다.
func TestInfo_String_Formatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   Info
		wantStr string
	}{
		{
			name: "완전한 정보 (Complete Info)",
			input: Info{
				Version:     "v1.0.0",
				Commit:      "1234567890abcdef",
				BuildDate:   "2025-01-01",
				BuildNumber: "1",
				GoVersion:   "go1.21",
				OS:          "linux",
				Arch:        "amd64",
			},
			wantStr: "v1.0.0 (commit: 1234567, build: 1, date: 2025-01-01, go_version: go1.21, os: linux, arch: amd64)",
		},
		{
			name: "변경사항이 있는 빌드 (Dirty Info -> +dirty)",
			input: Info{
				Version:    "v1.0.0",
				DirtyBuild: true,
				GoVersion:  "go1.21",
				OS:         "linux",
				Arch:       "amd64",
			},
			// 중요: Dirty 빌드는 SemVer Build Metadata 규격인 '+'를 사용해야 함
			wantStr: "v1.0.0+dirty (go_version: go1.21, os: linux, arch: amd64)",
		},
		{
			name: "최소 정보 (Minimal Info)",
			input: Info{
				Version: "v2.0.0",
			},
			wantStr: "v2.0.0",
		},
		{
			name:    "빈 정보 (Empty Info)",
			input:   Info{},
			wantStr: unknown,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantStr, tt.input.String())
		})
	}
}

// TestSet_Pure는 set 함수의 순수성(Side-effect Free)을 검증합니다.
// set은 입력을 그대로 저장해야 하며, 내부적으로 값을 변경하거나 채워넣으면 안 됩니다.
func TestSet_Pure(t *testing.T) {
	// Cleanup을 통해 테스트 종료 후 전역 상태 복구
	original := Get()
	t.Cleanup(func() { set(original) })

	// 초기화
	set(Info{})

	input := Info{Version: "v1.0.0"}
	set(input)

	got := Get()
	assert.Equal(t, "v1.0.0", got.Version)
	assert.Empty(t, got.Commit, "set()은 명시되지 않은 필드를 채워서는 안 됩니다.")
	assert.Empty(t, got.GoVersion, "set()은 런타임 정보를 자동으로 채워서는 안 됩니다.")
}

// TestEnrichBuildInfo는 빌드 정보 보강 로직(Business Logic)을 검증합니다.
// 런타임 환경 감지 및 디버그 정보 파싱이 올바르게 수행되는지 확인합니다.
func TestEnrichBuildInfo(t *testing.T) {
	// enrichBuildInfo 내부에서 readBuildInfo(패키지 변수)를 사용하므로
	// 동시성 문제 방지를 위해 t.Parallel()을 사용하지 않습니다.

	tests := []struct {
		name          string
		input         Info
		mockBuildInfo func() (*debug.BuildInfo, bool)
		wantInfo      Info
		checkRuntime  bool // GoVersion, OS, Arch 자동 채움 여부 확인
	}{
		{
			name:  "기본값 채움 (All Missing)",
			input: Info{Version: "v1.0.0"},
			mockBuildInfo: func() (*debug.BuildInfo, bool) {
				return nil, false
			},
			wantInfo: Info{
				Version:    "v1.0.0",
				Commit:     unknown,
				DirtyBuild: false,
			},
			checkRuntime: true,
		},
		{
			name:  "버전 없음 -> Unknown (Fallback)",
			input: Info{Version: ""},
			mockBuildInfo: func() (*debug.BuildInfo, bool) {
				return nil, false
			},
			wantInfo: Info{
				Version:    unknown,
				Commit:     unknown,
				DirtyBuild: false,
			},
			checkRuntime: true,
		},
		{
			name: "기존 정보 유지 (Pre-filled)",
			input: Info{
				Version:    "v2.0.0",
				Commit:     "abcdef",
				GoVersion:  "custom-go",
				OS:         "custom-os",
				Arch:       "custom-arch",
				DirtyBuild: true,
			},
			mockBuildInfo: func() (*debug.BuildInfo, bool) {
				return nil, false
			},
			wantInfo: Info{
				Version:    "v2.0.0",
				Commit:     "abcdef",
				GoVersion:  "custom-go",
				OS:         "custom-os",
				Arch:       "custom-arch",
				DirtyBuild: true,
			},
			checkRuntime: false,
		},
		{
			name: "Dirty 플래그 보정 (Correction)",
			input: Info{
				Version:    "v2.1.0",
				Commit:     "123456",
				DirtyBuild: false, // 잘못된 상태
			},
			mockBuildInfo: func() (*debug.BuildInfo, bool) {
				// 런타임 분석 결과가 수정됨(modified=true)을 가리킬 때
				return &debug.BuildInfo{
					Settings: []debug.BuildSetting{
						{Key: "vcs.modified", Value: "true"},
					},
				}, true
			},
			wantInfo: Info{
				Version:    "v2.1.0",
				Commit:     "123456",
				DirtyBuild: true, // true로 보정되어야 함
			},
			checkRuntime: false,
		},
		{
			name:  "Commit 'none' 정규화",
			input: Info{Version: "v3.0.0", Commit: "none"},
			mockBuildInfo: func() (*debug.BuildInfo, bool) {
				return nil, false
			},
			wantInfo: Info{
				Version: "v3.0.0",
				Commit:  unknown,
			},
			checkRuntime: true,
		},
		{
			name:  "VCS 정보로 보강 (Enrichment Success)",
			input: Info{Version: "v4.0.0"}, // Commit 누락
			mockBuildInfo: func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{
					Settings: []debug.BuildSetting{
						{Key: "vcs.revision", Value: "git-hash-123"},
						{Key: "vcs.time", Value: "2025-05-05"},
						{Key: "vcs.modified", Value: "true"},
					},
				}, true
			},
			wantInfo: Info{
				Version:    "v4.0.0",
				Commit:     "git-hash-123",
				BuildDate:  "2025-05-05",
				DirtyBuild: true,
			},
			checkRuntime: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			mockReadBuildInfo(t, tt.mockBuildInfo)

			got := enrichBuildInfo(tt.input)

			// 1. Static Fields assertions
			assert.Equal(t, tt.wantInfo.Version, got.Version)
			assert.Equal(t, tt.wantInfo.Commit, got.Commit)
			assert.Equal(t, tt.wantInfo.BuildDate, got.BuildDate)
			assert.Equal(t, tt.wantInfo.DirtyBuild, got.DirtyBuild)

			// 2. Runtime Fields assertions
			if tt.checkRuntime {
				assert.Equal(t, runtime.Version(), got.GoVersion, "GoVersion auto-population failed")
				assert.Equal(t, runtime.GOOS, got.OS, "OS auto-population failed")
				assert.Equal(t, runtime.GOARCH, got.Arch, "Arch auto-population failed")
			} else {
				if tt.wantInfo.GoVersion != "" {
					assert.Equal(t, tt.wantInfo.GoVersion, got.GoVersion)
				}
				if tt.wantInfo.OS != "" {
					assert.Equal(t, tt.wantInfo.OS, got.OS)
				}
				if tt.wantInfo.Arch != "" {
					assert.Equal(t, tt.wantInfo.Arch, got.Arch)
				}
			}
		})
	}
}

// mockReadBuildInfo는 테스트 기간 동안 readBuildInfo 변수를 안전하게 교체합니다.
func mockReadBuildInfo(t *testing.T, impl func() (*debug.BuildInfo, bool)) {
	t.Helper()
	original := readBuildInfo
	t.Cleanup(func() { readBuildInfo = original })
	readBuildInfo = impl
}

// TestHelpers는 Version()과 Commit() 헬퍼 함수를 검증합니다.
func TestHelpers(t *testing.T) {
	original := Get()
	t.Cleanup(func() { set(original) })

	set(Info{
		Version: "v1.5.0",
		Commit:  "deadbeef",
	})

	assert.Equal(t, "v1.5.0", Version())
	assert.Equal(t, "deadbeef", Commit())
}

// TestJSONMarshaling은 Info 구조체의 JSON 태그가 올바른지 검증합니다.
func TestJSONMarshaling(t *testing.T) {
	t.Parallel()
	info := Info{
		Version:     "v1.0.0",
		Commit:      "hash123",
		BuildNumber: "123",
		DirtyBuild:  true,
		GoVersion:   "go1.22",
		OS:          "darwin",
		Arch:        "arm64",
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// JSON 태그와 일치하는지 확인
	assert.Equal(t, "v1.0.0", decoded["version"])
	assert.Equal(t, "hash123", decoded["commit"])
	assert.Equal(t, "123", decoded["build_number"])
	assert.Equal(t, true, decoded["dirty_build"])
	assert.Equal(t, "go1.22", decoded["go_version"])
	assert.Equal(t, "darwin", decoded["os"])
	assert.Equal(t, "arm64", decoded["arch"])
}

// TestToMap은 로깅용 맵 변환이 Info 구조체 및 JSON 태그와 일관성을 유지하는지 검증합니다.
// 이는 구조화된 로깅(Structured Logging) 시 키 불일치를 방지하기 위함입니다.
func TestToMap_Consistency(t *testing.T) {
	t.Parallel()

	info := Info{
		Version:     "v1.2.3",
		Commit:      "abcdef",
		BuildDate:   "2025-01-01",
		BuildNumber: "999",
		GoVersion:   "go1.21",
		OS:          "linux",
		Arch:        "amd64",
		DirtyBuild:  true,
	}

	// 1. ToMap 결과 확인
	m := info.ToMap()
	assert.Equal(t, "v1.2.3", m["version"], "Version mismatch")
	assert.Equal(t, "abcdef", m["commit"], "Commit mismatch")
	assert.Equal(t, true, m["dirty_build"], "DirtyBuild mismatch")

	// 2. JSON Mashal 결과와 키 비교 (Consistency Check)
	jsonData, _ := json.Marshal(info)
	var jsonMap map[string]any
	json.Unmarshal(jsonData, &jsonMap)

	for k, v := range m {
		// ToMap의 키가 JSON 태그와 일치하는지, 값은 동일한지 확인
		// 주의: JSON 언마샬링 시 숫자는 float64가 될 수 있으므로 단순 값 비교는 주의
		if val, ok := jsonMap[k]; ok {
			assert.Equal(t, val, v, "ToMap value for key '%s' differs from JSON value", k)
		} else {
			t.Errorf("ToMap key '%s' not found in JSON output", k)
		}
	}
}

// =============================================================================
// Concurrency Safety Tests
// =============================================================================

// TestConcurrentAccess는 Race Detector와 함께 실행되어야 의미가 있습니다.
// go test -race ./...
func TestConcurrentAccess(t *testing.T) {
	const (
		numReaders = 100
		numWriters = 10
		iterations = 1000
	)

	var wg sync.WaitGroup
	wg.Add(numReaders + numWriters)

	// 테스트 종료 후 복구
	original := Get()
	t.Cleanup(func() { set(original) })

	set(Info{Version: "initial"})

	// Writers
	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				set(Info{
					Version:     fmt.Sprintf("v1.%d.%d", id, j),
					Commit:      fmt.Sprintf("commit-%d-%d", id, j),
					BuildNumber: fmt.Sprintf("%d", j),
				})
				runtime.Gosched()
			}
		}(i)
	}

	// Readers
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				info := Get()
				// 단순 필드 접근 시 Panic이 없는지 확인
				_ = info.Version
				_ = info.String()
			}
		}()
	}

	wg.Wait()
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkGet(b *testing.B) {
	original := Get()
	b.Cleanup(func() { set(original) })

	set(Info{
		Version:     "v1.0.0",
		Commit:      "benchmark-commit",
		BuildDate:   "2025-01-01",
		BuildNumber: "12345",
	})
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = Get()
		}
	})
}
