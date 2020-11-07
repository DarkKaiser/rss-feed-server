package crawling

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
	"io/ioutil"
	"net/http"
	"strings"
)

//noinspection GoUnhandledErrorResult
func httpWebPageDocument(url, title string, decoder *encoding.Decoder) (*goquery.Document, string, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("%s 접근이 실패하였습니다.", title), fmt.Errorf("HTTP Response StatusCode %d", res.StatusCode)
	}

	bodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Sprintf("%s의 내용을 읽을 수 없습니다.", title), err
	}
	defer res.Body.Close()

	if decoder != nil {
		bodyString, err := decoder.String(string(bodyBytes))
		if err != nil {
			return nil, fmt.Sprintf("%s의 문자열 변환이 실패하였습니다.", title), err
		}

		root, err := html.Parse(strings.NewReader(bodyString))
		if err != nil {
			return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
		}

		return goquery.NewDocumentFromNode(root), "", nil
	} else {
		root, err := html.Parse(strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, fmt.Sprintf("%s의 HTML 파싱이 실패하였습니다.", title), err
		}

		return goquery.NewDocumentFromNode(root), "", nil
	}
}
