package crawling

import log "github.com/sirupsen/logrus"

type naverCafeCrawlingBoard struct {
	id               string
	name             string
	boardType        string
	contentCanBeRead bool
}

type naverCafeCrawling struct {
	id          string
	clubID      string
	name        string
	description string
	url         string

	boards []*naverCafeCrawlingBoard
}

func (c *naverCafeCrawling) Run() {
	// @@@@@
	log.Print("naverCafeCrawling run~~~~~~~~~~")
}
