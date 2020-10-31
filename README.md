# rss-feed-server

### 설치 위치
* 라즈베리파이의 `/usr/local/go-workspace/src/github.com/darkkaiser/rss-feed-server/`에 설치

### 실행
* 재부팅시 실행되도록 crontab에 등록해 놓았음!   
  `@reboot sleep 20 && /usr/local/go-workspace/src/github.com/darkkaiser/rss-feed-server/rss-feed-server.sh`

### SSL 인증서
* 인증서 위치   
  `/etc/letsencrypt/live/darkkaiser.com`

* 매월 1일 자동 갱신되도록 crontab에 등록해 놓았음!   
  `0 1 * * * /usr/local/bin/certbot-auto renew --quiet`
