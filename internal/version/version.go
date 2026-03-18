// Package version 애플리케이션의 빌드 및 버저닝 정보를 관리하는 패키지입니다.
//
// 시스템의 빌드 시점(Build-Time)에 주입된 메타데이터(버전, 커밋 해시, 빌드 시간 등)와
// 실행 시점(Run-Time)의 환경 정보(Go 버전, OS, 아키텍처)를 통합하여 제공합니다.
//
// 주요 기능:
//  1. 빌드 정보 주입: 링커 플래그(-ldflags)를 통해 외부에서 버전을 주입받습니다.
//  2. 런타임 정보 통합: 실행 환경의 정보를 자동으로 감지하여 추가합니다.
//  3. Thread-Safe 접근: 전역적으로 안전하게 정보를 조회할 수 있는 메서드를 제공합니다.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
)

const (
	unknown = "unknown"
	none    = "none"
)

// globalBuildInfo 전역 빌드 정보 (Atomic Value를 사용하여 Thread-Safe 보장)
var globalBuildInfo atomic.Value

// readBuildInfo 테스트에서 교체(Mocking) 가능하도록 변수로 선언합니다.
var readBuildInfo = debug.ReadBuildInfo

// -----------------------------------------------------------------------------
// 빌드 정보 변수
// -----------------------------------------------------------------------------
// 다음 변수들은 Dockerfile에서 컴파일 시점에 링커 플래그(ldflags)를 통해 주입됩니다.
//
// 주의: 이 변수들은 외부에서 값을 주입받기 위한 '컨테이너' 역할만 수행합니다.
// 실제 애플리케이션 로직에서는 이 변수들에 직접 접근하지 말고,
// 반드시 Get() 함수나 Info 구조체를 통해 안전하게 접근해야 합니다.
var (
	appVersion    = "" // 애플리케이션 버전 (예: v1.0.1-155-gf25b8bf)
	gitCommitHash = "" // Git 커밋 해시 (예: f25b8bf)
	gitTreeState  = "" // Git 작업 트리의 변경 상태 (clean 또는 dirty)
	buildDate     = "" // 빌드 수행 시간
	buildNumber   = "" // CI/CD 파이프라인 빌드 번호
)

// init 애플리케이션의 빌드 정보를 초기화합니다.
func init() {
	bi := Info{
		Version:     strings.TrimSpace(appVersion),
		Commit:      strings.TrimSpace(gitCommitHash),
		BuildDate:   strings.TrimSpace(buildDate),
		BuildNumber: strings.TrimSpace(buildNumber),
	}

	if strings.ToLower(strings.TrimSpace(gitTreeState)) == "dirty" {
		bi.DirtyBuild = true
	}

	set(enrichBuildInfo(bi))
}

// Info 애플리케이션의 빌드 정보를 담고 있습니다.
//
// 이 구조체는 애플리케이션의 버전, 빌드 날짜, 빌드 번호 등의 메타데이터를 저장합니다.
// 주로 /version API 엔드포인트나 로그 출력에 사용됩니다.
type Info struct {
	Version     string `json:"version"`      // 애플리케이션의 버전 (예: v1.0.1-155-gf25b8bf)
	Commit      string `json:"commit"`       // Git 커밋 해시 (예: f25b8bf)
	BuildDate   string `json:"build_date"`   // 빌드 날짜 (ISO 8601 형식 권장, 예: "2025-12-05T11:30:00Z")
	BuildNumber string `json:"build_number"` // CI/CD 빌드 번호 (예: "456")
	GoVersion   string `json:"go_version"`   // 빌드에 사용된 Go 컴파일러 버전 (예: "go1.21.0")
	OS          string `json:"os"`           // 실행 중인 운영체제 (예: "linux", "darwin", "windows")
	Arch        string `json:"arch"`         // 실행 중인 시스템 아키텍처 (예: "amd64")
	DirtyBuild  bool   `json:"dirty_build"`  // 빌드 시점에 로컬 소스코드에서 변경사항이 있었는지의 여부
}

// Get 애플리케이션의 빌드 정보를 반환합니다.
func Get() Info {
	bi := globalBuildInfo.Load()
	if bi == nil {
		return Info{
			Version:     unknown,
			Commit:      unknown,
			BuildDate:   unknown,
			BuildNumber: "0",
		}
	}
	return bi.(Info)
}

// set 애플리케이션의 빌드 정보를 설정합니다.
func set(bi Info) {
	globalBuildInfo.Store(bi)
}

// enrichBuildInfo 초기화되지 않은 빌드 정보에 런타임 환경 값(Go 버전, OS, Arch)을 채워 넣습니다.
//
// 또한, 버전 정보가 누락된 경우 실행 파일의 디버그 메타데이터(debug.ReadBuildInfo)를 분석하여
// VCS 리비전이나 수정 상태(Dirty) 등의 정보를 보강(Enrich)하는 역할을 수행합니다.
// 순수 함수(Pure Function)로 설계되어 단위 테스트가 용이합니다.
func enrichBuildInfo(bi Info) Info {
	if bi.GoVersion == "" {
		bi.GoVersion = runtime.Version()
	}
	if bi.OS == "" {
		bi.OS = runtime.GOOS
	}
	if bi.Arch == "" {
		bi.Arch = runtime.GOARCH
	}

	// Go 모듈(debug.BuildInfo)을 통해 VCS(Git) 메타데이터 추출을 시도합니다.
	// 이는 -ldflags 주입이 누락된 개발 환경(go run 등)에서도 최소한의 버전 정보를 확보하기 위함이며,
	// ldflags가 있더라도 DirtyBuild 여부 등을 교차 검증하기 위해 항상 실행합니다.
	if val, ok := readBuildInfo(); ok {
		for _, setting := range val.Settings {
			switch setting.Key {
			case "vcs.revision":
				// 외부에서 주입된 값이 없거나 "none"일 경우에만 덮어씀
				if bi.Commit == "" || bi.Commit == unknown || bi.Commit == none {
					bi.Commit = setting.Value
				}
			case "vcs.time":
				if bi.BuildDate == "" || bi.BuildDate == unknown {
					bi.BuildDate = setting.Value
				}
			case "vcs.modified":
				if setting.Value == "true" {
					bi.DirtyBuild = true
				}
			}
		}
		// 여전히 버전이 비어있다면 Main 모듈 버전 사용 시도
		if bi.Version == "" && val.Main.Version != "(devel)" {
			bi.Version = val.Main.Version
		}
	}

	// 최종적으로 값이 없는 필드에 기본값(unknown) 할당
	if bi.Version == "" {
		bi.Version = unknown
	}
	if bi.Commit == "" || bi.Commit == none {
		bi.Commit = unknown
	}

	return bi
}

// Version 애플리케이션의 버전 문자열을 반환합니다.
func Version() string {
	return Get().Version
}

// Commit Git 커밋 해시를 반환합니다.
func Commit() string {
	return Get().Commit
}

// ToMap 빌드 정보를 맵(Map) 형태로 반환합니다. (구조적 로깅용)
func (i Info) ToMap() map[string]any {
	return map[string]any{
		"version":      i.Version,
		"commit":       i.Commit,
		"build_date":   i.BuildDate,
		"build_number": i.BuildNumber,
		"go_version":   i.GoVersion,
		"os":           i.OS,
		"arch":         i.Arch,
		"dirty_build":  i.DirtyBuild,
	}
}

// String 빌드 정보를 사람이 읽기 쉬운 하나의 문자열로 요약해 반환합니다.
func (i Info) String() string {
	if i.Version == "" {
		return unknown
	}
	version := i.Version
	if i.DirtyBuild {
		version += "+dirty"
	}

	var details []string

	// 커밋 해시 (unknown이 아니면 포함)
	if i.Commit != "" && i.Commit != unknown {
		commit := i.Commit
		if len(commit) > 7 {
			commit = commit[:7]
		}
		details = append(details, fmt.Sprintf("commit: %s", commit))
	}

	// 빌드 번호 (값이 있을 때만 포함)
	if i.BuildNumber != "" {
		details = append(details, fmt.Sprintf("build: %s", i.BuildNumber))
	}

	// 빌드 날짜 (값이 있고 unknown이 아니면 포함)
	if i.BuildDate != "" && i.BuildDate != unknown {
		details = append(details, fmt.Sprintf("date: %s", i.BuildDate))
	}

	// Go 버전 (값이 있을 때만 포함)
	if i.GoVersion != "" {
		details = append(details, fmt.Sprintf("go_version: %s", i.GoVersion))
	}

	// OS/Arch (값이 있을 때만 포함)
	if i.OS != "" {
		details = append(details, fmt.Sprintf("os: %s", i.OS))
	}
	if i.Arch != "" {
		details = append(details, fmt.Sprintf("arch: %s", i.Arch))
	}

	if len(details) == 0 {
		return version
	}

	return fmt.Sprintf("%s (%s)", version, strings.Join(details, ", "))
}
