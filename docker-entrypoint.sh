#!/bin/bash
set -euo pipefail

# 로깅 함수
log_info() {
  echo "[$(date +'%Y-%m-%d %H:%M:%S')] [INFO] $*"
}

log_error() {
  echo "[$(date +'%Y-%m-%d %H:%M:%S')] [ERROR] $*" >&2
}

log_warn() {
  echo "[$(date +'%Y-%m-%d %H:%M:%S')] [WARN] $*" >&2
}

# 애플리케이션 설정
APP_NAME=rss-feed-server
APP_PATH=/usr/local/app/
APP_BIN_FILE=${APP_PATH}${APP_NAME}
APP_CONFIG_FILE=${APP_PATH}${APP_NAME}.json

LATEST_APP_BIN_FILE=/docker-entrypoint/dist/${APP_NAME}
LATEST_APP_CONFIG_FILE=/docker-entrypoint/dist/${APP_NAME}.json

log_info "Docker entrypoint 스크립트 시작..."

# 바이너리 파일 처리
if [ -f "$LATEST_APP_BIN_FILE" ]; then
  log_info "애플리케이션 바이너리를 $APP_PATH 로 이동 중..."
  mv -f "$LATEST_APP_BIN_FILE" "$APP_PATH" || {
    log_error "바이너리 파일 이동 실패: $LATEST_APP_BIN_FILE -> $APP_PATH"
    exit 1
  }
  
  # 실행 권한 확인
  if [ ! -x "$APP_BIN_FILE" ]; then
    log_warn "바이너리 파일에 실행 권한이 없습니다. 권한을 부여합니다."
    chmod +x "$APP_BIN_FILE"
  fi
  
  log_info "바이너리 이동 완료"
else
  log_warn "바이너리 파일을 찾을 수 없습니다: $LATEST_APP_BIN_FILE"
fi

# 설정 파일 처리
if [ -f "$LATEST_APP_CONFIG_FILE" ]; then
  log_info "설정 파일을 $APP_PATH 로 이동 중..."
  mv -f "$LATEST_APP_CONFIG_FILE" "$APP_PATH" || {
    log_error "설정 파일 이동 실패: $LATEST_APP_CONFIG_FILE -> $APP_PATH"
    exit 1
  }
  
  # JSON 파일 유효성 검증 (간단한 검사)
  if command -v jq >/dev/null 2>&1; then
    if ! jq empty "$APP_CONFIG_FILE" >/dev/null 2>&1; then
      log_error "설정 파일이 유효한 JSON 형식이 아닙니다: $APP_CONFIG_FILE"
      exit 1
    fi
    log_info "설정 파일 검증 완료"
  else
    log_warn "jq가 설치되지 않아 JSON 검증을 건너뜁니다."
  fi
  
  log_info "설정 파일 이동 완료"
else
  log_warn "설정 파일을 찾을 수 없습니다: $LATEST_APP_CONFIG_FILE"
fi

# 최종 확인
if [ ! -f "$APP_BIN_FILE" ]; then
  log_error "애플리케이션 바이너리가 존재하지 않습니다: $APP_BIN_FILE"
  exit 1
fi

if [ ! -f "$APP_CONFIG_FILE" ]; then
  log_error "설정 파일이 존재하지 않습니다: $APP_CONFIG_FILE"
  exit 1
fi

log_info "모든 파일 준비 완료"
log_info "애플리케이션 시작: $*"
exec "$@"
