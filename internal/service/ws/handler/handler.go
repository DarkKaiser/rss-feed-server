package handler

import (
	"github.com/darkkaiser/notify-server/pkg/notify"
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/store/sqlite"
)

type Handler struct {
	config *config.AppConfig

	rssFeedProviderStore *sqlite.Store

	notifyClient *notify.Client
}

func NewHandler(config *config.AppConfig, rssFeedProviderStore *sqlite.Store, notifyClient *notify.Client) *Handler {
	return &Handler{
		config: config,

		rssFeedProviderStore: rssFeedProviderStore,

		notifyClient: notifyClient,
	}
}
