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

네이버 카페, 여수시청 게시판의 게시글을 크롤링하여 RSS 피드 서비스를 제공합니다.

## Build

```bash
docker build -t darkkaiser/rss-feed-server .
```

## Run

```bash
docker ps -q --filter name=rss-feed-server | grep -q . && docker container stop rss-feed-server && docker container rm rss-feed-server

docker run -d --name rss-feed-server \
              -e TZ=Asia/Seoul \
              -v /usr/local/docker/rss-feed-server:/usr/local/app \
              -v /etc/letsencrypt/:/etc/letsencrypt/ \
              -p 443:443 \
              --restart="always" \
              darkkaiser/rss-feed-server
```

## SSL 인증서

```
/etc/letsencrypt/live/darkkaiser.com
```

## 🤝 Contributing

Contributions, issues and feature requests are welcome.<br />
Feel free to check [issues page](https://github.com/DarkKaiser/rss-feed-server/issues) if you want to contribute.

## Author

👤 **DarkKaiser**

- Blog: [@DarkKaiser](http://www.darkkaiser.com)
- Github: [@DarkKaiser](https://github.com/DarkKaiser)
