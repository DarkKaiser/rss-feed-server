package handler

import (
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
)

type Handler struct {
	config *config.AppConfig

	rssFeedProviderStore feed.Repository

	notifyClient *notify.Client
}

func NewHandler(config *config.AppConfig, rssFeedProviderStore feed.Repository, notifyClient *notify.Client) *Handler {
	return &Handler{
		config: config,

		rssFeedProviderStore: rssFeedProviderStore,

		notifyClient: notifyClient,
	}
}
