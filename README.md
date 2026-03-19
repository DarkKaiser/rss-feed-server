# RSS Feed Server

<p>
  <img src="https://img.shields.io/badge/Go-00ADD8?style=flat&logo=Go&logoColor=white" />
  <img src="https://img.shields.io/badge/Echo-02303A?style=flat&logo=Echo&logoColor=white">
  <img src="https://img.shields.io/badge/SQLite-003B57?style=flat&logo=SQLite&logoColor=white">
  <img src="https://img.shields.io/badge/jenkins-%232C5263.svg?style=flat&logo=jenkins&logoColor=white">
  <img src="https://img.shields.io/badge/Docker-2496ED?style=flat&logo=Docker&logoColor=white">
  <img src="https://img.shields.io/badge/Linux-FCC624?style=flat&logo=linux&logoColor=black">
  <a href="https://github.com/DarkKaiser/rss-feed-server/blob/main/LICENSE">
    <img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-yellow.svg" target="_blank" />
  </a>
</p>

다양한 목적의 웹 게시판(네이버 카페, 여수시청, 여수 쌍봉초등학교 등)의 새로운 게시글을 자동으로 모니터링 및 크롤링하여 **RSS 2.0 피드**로 일원화해 제공하는 서비스입니다. Docker 컨테이너 기반으로 동작하며 SSL/TLS를 통한 보안 통신을 지원합니다.

## 📌 주요 기능

- **다양한 게시판 지원 및 자동 크롤링**
  - 네이버 카페 (다수 채널 지원 가능)
  - 여수시청 소식
  - 여수 쌍봉초등학교 소식
- **주기적 크롤링 엔진 내장**
  - 설정된 주기에 따라 백그라운드에서 게시글을 최신화하여 자체 DB(SQLite) 구축
- **표준화된 RSS 2.0 제공**
  - 게시글 제목, 내용, 작성일 등 상세 정보 포함
  - RSS 리더기를 통한 새 글 알림 구독 지원
- **API 문서화 (Swagger)**
  - 내장된 Swagger UI를 통해 API 스펙 및 상태 정보 확인 가능

## 🛠 기술 스택

- **Language & Framework**: Go, Echo
- **Database**: SQLite
- **Documentation**: Swaggo
- **Scheduler**: cron (robfig/cron)
- **Infrastructure**: Docker, Nginx Proxy Manager, Jenkins

## 🚀 설치 및 실행

### Docker 이미지 빌드

```bash
docker build -t darkkaiser/rss-feed-server .
```

### Docker 컨테이너 실행

```bash
# 기존 컨테이너가 있다면 제거
docker ps -q --filter name=rss-feed-server | grep -q . && docker container stop rss-feed-server && docker container rm rss-feed-server

# 컨테이너 구동
docker run -d --name rss-feed-server \
              -e TZ=Asia/Seoul \
              -v /usr/local/docker/rss-feed-server:/usr/local/app \
              -v /usr/local/docker/nginx-proxy-manager/letsencrypt:/etc/letsencrypt:ro \
              -p 3443:3443 \
              --add-host=api.darkkaiser.com:192.168.219.110 \
              --restart="always" \
              darkkaiser/rss-feed-server
```

## 🔒 SSL 인증서

SSL 인증서는 Nginx Proxy Manager를 통해 발급된 Let's Encrypt 인증서를 사용합니다.
인증서 갱신은 Nginx Proxy Manager 내에서 자동으로 처리됩니다.
- 인증서 위치: `/usr/local/docker/nginx-proxy-manager/letsencrypt/live/npm-1`

## 📖 API 엔드포인트 및 문서

API의 자세한 명세와 테스트는 내장된 Swagger 문서를 통해 확인하실 수 있습니다.
- **Swagger UI**: `https://rss.darkkaiser.com:3443/swagger/index.html`
- **RSS 전체 피드 요약 조회**: `https://rss.darkkaiser.com:3443/`

주요 RSS 피드 예시 (`GET /<id>`):
- 네이버 카페 (ludypang): `https://rss.darkkaiser.com:3443/ludypang.xml`
- 여수시청: `https://rss.darkkaiser.com:3443/yeosu-cityhall-news.xml`
- 여수 쌍봉초등학교: `https://rss.darkkaiser.com:3443/ssangbong-elementary-school-news.xml`

## 🤝 Contributing

Contributions, issues and feature requests are welcome.<br />
Feel free to check [issues page](https://github.com/DarkKaiser/rss-feed-server/issues) if you want to contribute.

## 👤 Author

**DarkKaiser**
- Blog: [@DarkKaiser](https://www.darkkaiser.com)
- Github: [@DarkKaiser](https://github.com/DarkKaiser)

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
