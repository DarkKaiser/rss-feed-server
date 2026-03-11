# RssFeedServer

<p>
  <img src="https://img.shields.io/badge/Go-00ADD8?style=flat&logo=Go&logoColor=white" />
  <img src="https://img.shields.io/badge/jenkins-%232C5263.svg?style=flat&logo=jenkins&logoColor=white">
  <img src="https://img.shields.io/badge/Docker-2496ED?style=flat&logo=Docker&logoColor=white">
  <img src="https://img.shields.io/badge/Linux-FCC624?style=flat&logo=linux&logoColor=black">
  <a href="https://github.com/DarkKaiser/rss-feed-server/blob/main/LICENSE">
    <img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-yellow.svg" target="_blank" />
  </a>
</p>

네이버 카페와 여수시청 게시판의 새로운 게시글을 자동으로 크롤링하여 RSS 피드로 제공하는 서비스입니다. Docker 기반으로 동작하며 SSL/TLS를 통한 보안 통신을 지원합니다.

## 주요 기능

- 네이버 카페 게시글 자동 크롤링 및 RSS 피드 생성
  - 설정된 주기에 따라 자동 수집
  - 게시글 제목, 내용, 작성일 등 정보 제공
- 여수시청 게시판 RSS 피드 생성
  - HTTPS 지원
  - Docker 컨테이너 기반 실행
  
## 설치 전 필요사항
설치 전 필요사항ck설치 전 필요사항
-설치 전 필요사항인증서설치 전 필요사항's 설치 전 필요사항pt 설치 전 필요사항pt8설치 전 필요사항pt 설치 전 필요사항pt설치 전 필요사항ptld

```bash
docker build -t darkkaiser/rss-feed-server .
```

## Run

```bash
docker ps -q --filter name=rss-feed-server | grep -q . && docker container stop rss-feed-server && docker container rm rss-feed-server

docker run -d --name rss-feed-server \
              -e TZ=Asia/Seoul \
              -v /usr/local/docker/rss-feed-server:/usr/local/app \
              -v /usr/local/docker/nginx-proxy-manager/letsencrypt:/etc/letsencrypt:ro \
              -p 3443:3443 \
              --add-host=api.darkkaiser.com:192.168.219.110 \
              --restart="always" \
              darkkaiser/rss-feed-server
```

## SSL 인증서

SSL 인증서는 Nginx Proxy Manager를 통해 발급된 인증서를 사용합니다.

인증서 위치:
```
/usr/local/docker/nginx-proxy-manager/letsencrypt/live/npm-1
```

인증서 갱신은 Nginx Proxy Manager에서 자동으로 처리됩니다.

## API 엔드포인트

- RSS 피드
  - 네이버 카페: `https://<hostname>:3443/rss/naver/cafe/<카페ID>`
  - 여수시청: `https://<hostname>:3443/rss/yeosu/notice`

## 🤝 Contributing

Contributions, issues and feature requests are welcome.<br />
Feel free to check [issues page](https://github.com/DarkKaiser/rss-feed-server/issues) if you want to contribute.

## Author

👤 **DarkKaiser**

- Blog: [@DarkKaiser](https://www.darkkaiser.com)
- Github: [@DarkKaiser](https://github.com/DarkKaiser)

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
