package utils

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"regexp"
	"strings"
)

func CheckErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func ToSnakeCase(str string) string {
	matchFirstRegexp := regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllRegexp := regexp.MustCompile("([a-z0-9])([A-Z])")

	snakeCaseString := matchFirstRegexp.ReplaceAllString(str, "${1}_${2}")
	snakeCaseString = matchAllRegexp.ReplaceAllString(snakeCaseString, "${1}_${2}")

	return strings.ToLower(snakeCaseString)
}

func Contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

func Trim(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func TrimMultiLine(s string) string {
	var ret []string
	var appendedEmptyLine bool

	lines := strings.Split(s, "\n")
	for _, line := range lines {
		trimLine := Trim(line)
		if trimLine != "" {
			appendedEmptyLine = false
			ret = append(ret, trimLine)
		} else {
			if appendedEmptyLine == false {
				appendedEmptyLine = true
				ret = append(ret, "")
			}
		}
	}

	if len(ret) >= 2 {
		if ret[0] == "" {
			ret = ret[1:]
		}
		if ret[len(ret)-1] == "" {
			ret = ret[:len(ret)-1]
		}
	}

	return strings.Join(ret, "\r\n")
}

func FormatCommas(num int) string {
	str := fmt.Sprintf("%d", num)
	re := regexp.MustCompile("(\\d+)(\\d{3})")
	for n := ""; n != str; {
		n = str
		str = re.ReplaceAllString(str, "$1,$2")
	}
	return str
}

func SplitExceptEmptyItems(s, sep string) []string {
	tokens := strings.Split(s, sep)

	var t []string
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token != "" {
			t = append(t, token)
		}
	}

	return t
}
