package notifyapi

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	validUrl           = "http://api.darkkaiser.com/api/notify/message/send"
	validAPIKey        = "ABCDEFG:12345"
	validApplicationID = "rss-feed-server"
)

func TestConfig_Validation(t *testing.T) {
	assert := assert.New(t)

	c := &Config{
		Url:           validUrl,
		APIKey:        validAPIKey,
		ApplicationID: validApplicationID,
	}

	assert.True(c.validation())
	assert.True(c.valid)

	for _, v := range []string{"", "   ", "ftp://", "HTTP://"} {
		c.Url = v
		assert.False(c.validation())
		assert.False(c.valid)
	}

	c.Url = validUrl

	for _, v := range []string{"", "   "} {
		c.APIKey = v
		assert.False(c.validation())
		assert.False(c.valid)
	}

	c.APIKey = validAPIKey

	for _, v := range []string{"", "   "} {
		c.ApplicationID = v
		assert.False(c.validation())
		assert.False(c.valid)
	}
}

func TestInit(t *testing.T) {
	assert := assert.New(t)

	c := &Config{
		Url:           validUrl,
		APIKey:        validAPIKey,
		ApplicationID: validApplicationID,
	}

	Init(c)
	assert.Same(c, config)
	assert.True(config.valid)

	for _, v := range []string{"", "   ", "ftp://", "HTTP://"} {
		c.Url = v

		Init(c)
		assert.Same(c, config)
		assert.False(config.valid)
	}

	c.Url = validUrl

	for _, v := range []string{"", "   "} {
		c.APIKey = v

		Init(c)
		assert.Same(c, config)
		assert.False(config.valid)
	}

	c.APIKey = validAPIKey

	for _, v := range []string{"", "   "} {
		c.ApplicationID = v

		Init(c)
		assert.Same(c, config)
		assert.False(config.valid)
	}
}

func TestSend(t *testing.T) {
	assert := assert.New(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)

		assert.Equal("Bearer "+validAPIKey, r.Header.Get("Authorization"))
		assert.Equal(fmt.Sprintf(`{"application_id":"%s","message":"메시지","error_occurred":false}`, validApplicationID), string(body))
	}))
	defer ts.Close()

	// 정상적으로 초기화되었을 경우...
	Init(&Config{
		Url:           ts.URL,
		APIKey:        validAPIKey,
		ApplicationID: validApplicationID,
	})

	assert.True(config.valid)
	assert.True(Send("메시지", false))

	// 빈 메시지를 넘겼을 경우...
	assert.False(Send("", false))

	// 유효하지 않은 설정값으로 초기화되었을 경우...
	Init(&Config{
		Url:           "",
		APIKey:        validAPIKey,
		ApplicationID: validApplicationID,
	})

	assert.False(config.valid)
	assert.False(Send("메시지", false))
}
