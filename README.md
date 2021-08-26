# rss-feed-server
네이버 카페 및 여수시청 게시판의 목록을 읽어서 RSS 피드 서비스를 제공합니다.

### 설치 위치
라즈베리파이의 `/usr/local/rss-feed-server/`에 설치

### 실행
재부팅시 자동 실행되도록 crontab에 등록해 놓았음!   
  `@reboot sleep 20 && /usr/local/rss-feed-server/rss-feed-server.sh`

### SSL 인증서
인증서 위치   
  `/etc/letsencrypt/live/darkkaiser.com`

매월 1일 자동 갱신되도록 crontab에 등록해 놓았음!   
`0 1 * * * /usr/local/bin/certbot-auto renew --quiet`
