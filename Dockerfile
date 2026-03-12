# ------------------------------------------
# 1. Build Image
# ------------------------------------------
FROM golang:1.24.0-bullseye AS builder

# 빌드 메타데이터 인자
ARG APP_NAME=rss-feed-server
ARG APP_VERSION=unknown
ARG GIT_COMMIT_HASH=unknown
ARG GIT_TREE_STATE=clean
ARG BUILD_DATE=unknown
ARG BUILD_NUMBER=unknown
ARG TARGETARCH

WORKDIR /go/src/app/

# 의존성 캐싱 최적화: go.mod와 go.sum을 먼저 복사
COPY go.mod go.sum ./
RUN go mod download

# 소스 코드 복사
COPY . .

# [CA 인증서 등록 1/2] 사설 CA 인증서를 OS 인증서 보관소(/usr/local/share/ca-certificates/)에 복사
# - 이 인증서가 있어야 해당 기관이 발급한 SSL 인증서를 신뢰할 수 있음
# - 권한 644: 소유자(root)만 수정 가능, 그 외 사용자는 읽기만 허용
COPY --chmod=644 ./secrets/CA/Sectigo_RSA_Domain_Validation_Secure_Server_CA.crt /usr/local/share/ca-certificates/

# [CA 인증서 등록 2/2] 복사된 인증서를 시스템 통합 인증서 파일(/etc/ssl/certs/ca-certificates.crt)에 병합하여 활성화
RUN /usr/sbin/update-ca-certificates

# 테스트 실행 (빌드 전 품질 검증)
RUN go test ./... -v -coverprofile=coverage.out

# golangci-lint 설치 및 실행
# 현재 다수의 린트 오류(errcheck, gosimple 등)로 인해 비활성화
# 린트 오류 수정은 별도 작업으로 진행 예정
# COPY --from=golangci/golangci-lint:v1.62.2 /usr/bin/golangci-lint /usr/bin/golangci-lint

# 린트 검사 실행 (실패 시 빌드 중단)
# RUN golangci-lint run ./...

# 빌드 정보를 바이너리에 주입
RUN CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w \
    -X 'github.com/darkkaiser/rss-feed-server/internal/pkg/version.appVersion=${APP_VERSION}' \
    -X 'github.com/darkkaiser/rss-feed-server/internal/pkg/version.gitCommitHash=${GIT_COMMIT_HASH}' \
    -X 'github.com/darkkaiser/rss-feed-server/internal/pkg/version.gitTreeState=${GIT_TREE_STATE}' \
    -X 'github.com/darkkaiser/rss-feed-server/internal/pkg/version.buildDate=${BUILD_DATE}' \
    -X 'github.com/darkkaiser/rss-feed-server/internal/pkg/version.buildNumber=${BUILD_NUMBER}'" \
    -o ${APP_NAME} ./cmd/rss-feed-server

# ------------------------------------------
# 2. Production Image
# ------------------------------------------
FROM debian:bullseye-slim

# 빌드 메타데이터 인자
ARG APP_NAME=rss-feed-server
ARG APP_VERSION=unknown
ARG GIT_COMMIT_HASH=unknown
ARG BUILD_DATE=unknown
ARG BUILD_NUMBER=unknown

# OCI 표준 레이블 추가
LABEL org.opencontainers.image.created="${BUILD_DATE}" \
    org.opencontainers.image.authors="DarkKaiser" \
    org.opencontainers.image.url="https://github.com/DarkKaiser/rss-feed-server" \
    org.opencontainers.image.source="https://github.com/DarkKaiser/rss-feed-server" \
    org.opencontainers.image.version="${APP_VERSION}" \
    org.opencontainers.image.revision="${GIT_COMMIT_HASH}" \
    org.opencontainers.image.title="RSS Feed Server" \
    org.opencontainers.image.description="웹 페이지 스크래핑 및 RSS 피드 제공 서버 (네이버 카페, 여수시청, 여수 쌍봉초등학교)" \
    build.number="${BUILD_NUMBER}"

# 필수 패키지 설치 및 사용자 생성을 하나의 레이어로 통합
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata wget jq && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -g 1000 appuser && \
    useradd -r -u 1000 -g appuser appuser && \
    mkdir -p /docker-entrypoint/dist /usr/local/app && \
    chown -R appuser:appuser /docker-entrypoint /usr/local/app

WORKDIR /docker-entrypoint/dist/

# 빌드 결과물 복사 (권한 설정 포함)
COPY --from=builder --chown=appuser:appuser /go/src/app/${APP_NAME} .

# 스크립트 복사 및 실행 권한 부여
COPY --chown=appuser:appuser --chmod=755 docker-entrypoint.sh /docker-entrypoint/

# 설정 파일 복사
COPY --chown=appuser:appuser ./secrets/${APP_NAME}.운영.json /docker-entrypoint/dist/${APP_NAME}.json

# SSL 인증서 복사
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# 작업 디렉토리 변경
WORKDIR /usr/local/app/

# 비루트 사용자로 전환
USER appuser

# 포트 노출
EXPOSE 3443

ENTRYPOINT ["/docker-entrypoint/docker-entrypoint.sh"]
CMD ["/usr/local/app/rss-feed-server"]
