package utils

import (
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCheckErr(t *testing.T) {
	cases := []struct {
		err      error
		expected bool
	}{
		{
			err:      nil,
			expected: false,
		}, {
			err:      errors.New("error"),
			expected: true,
		},
	}

	defer func() { log.StandardLogger().ExitFunc = nil }()

	var occurredFatal bool
	log.StandardLogger().ExitFunc = func(int) { occurredFatal = true }

	for _, c := range cases {
		occurredFatal = false
		CheckErr(c.err)

		assert.Equal(t, c.expected, occurredFatal)
	}
}

func TestToSnakeCase(t *testing.T) {
	cases := []struct {
		str      string
		expected string
	}{
		{str: "My", expected: "my"},
		{str: "123", expected: "123"},
		{str: "123abc", expected: "123abc"},
		{str: "123abcDef", expected: "123abc_def"},
		{str: "123abcDefGHI", expected: "123abc_def_ghi"},
		{str: "123abcDefGHIj", expected: "123abc_def_gh_ij"},
		{str: "123abcDefGHIjK", expected: "123abc_def_gh_ij_k"},
		{str: "MyNameIsTom", expected: "my_name_is_tom"},
		{str: "myNameIsTom", expected: "my_name_is_tom"},
		{str: " myNameIsTom ", expected: " my_name_is_tom "},
		{str: " myNameIsTom  yourNameIsB", expected: " my_name_is_tom  your_name_is_b"},
	}

	for _, c := range cases {
		assert.Equal(t, c.expected, ToSnakeCase(c.str))
	}
}

func TestContains(t *testing.T) {
	cases := []struct {
		list     []string
		item     string
		expected bool
	}{
		{list: []string{"A1", "B1", "C1"}, item: "", expected: false},
		{list: []string{"A1", "B1", "C1"}, item: "A1", expected: true},
		{list: []string{"A1", "B1", "C1"}, item: "a1", expected: false},
		{list: []string{"A1", "B1", "C1"}, item: "A2", expected: false},
		{list: []string{"A1", "B1", "C1"}, item: "A1 ", expected: false},
	}

	for _, c := range cases {
		assert.Equal(t, c.expected, Contains(c.list, c.item))
	}
}

func TestTrim(t *testing.T) {
	cases := []struct {
		s        string
		expected string
	}{
		{s: "테스트", expected: "테스트"},
		{s: "   테스트   ", expected: "테스트"},
		{s: "   하나 공백   ", expected: "하나 공백"},
		{s: "   다수    공백   ", expected: "다수 공백"},
		{s: "   다수    공백   여러개   ", expected: "다수 공백 여러개"},
		{s: "   @    특수문자   $   ", expected: "@ 특수문자 $"},
		{s: `
		
				라인    1
				라인2
		
		
				라인3
		
				라인4
		
		
				라인5

			`,
			expected: "라인 1 라인2 라인3 라인4 라인5"},
	}

	for _, c := range cases {
		assert.Equal(t, c.expected, Trim(c.s))
	}
}

func TestTrimMultiLine(t *testing.T) {
	cases := []struct {
		s        string
		expected string
	}{
		{s: "", expected: ""},
		{s: "   ", expected: ""},
		{s: "  a  ", expected: "a"},
		{s: `
		
				라인    1
				라인2
		
		
				라인3
		
				라인4
		
		
		
				라인5
		
		
			`,
			expected: "라인 1\r\n라인2\r\n\r\n라인3\r\n\r\n라인4\r\n\r\n라인5"},
		{s: ` 라인    1
		
		
			라인2
		
		
			라인3
			라인4
			라인5   `,
			expected: "라인 1\r\n\r\n라인2\r\n\r\n라인3\r\n라인4\r\n라인5"},
		{s: `



			`,
			expected: ""},
		{s: `

			1

			`,
			expected: "1"},
	}

	for _, c := range cases {
		assert.Equal(t, c.expected, TrimMultiLine(c.s))
	}
}

func TestFormatCommas(t *testing.T) {
	cases := []struct {
		num      int
		expected string
	}{
		{num: 0, expected: "0"},
		{num: 100, expected: "100"},
		{num: 1000, expected: "1,000"},
		{num: 1234567, expected: "1,234,567"},
		{num: -1234567, expected: "-1,234,567"},
	}

	for _, c := range cases {
		assert.Equal(t, c.expected, FormatCommas(c.num))
	}
}

func TestSplitExceptEmptyItems(t *testing.T) {
	var notAssign []string

	cases := []struct {
		s        string
		sep      string
		expected []string
	}{
		{s: "1,2,3", sep: ",", expected: []string{"1", "2", "3"}},
		{s: ",1,2,3,,,", sep: ",", expected: []string{"1", "2", "3"}},
		{s: ",1,  ,  ,2,3,,,", sep: ",", expected: []string{"1", "2", "3"}},
		{s: ",1,,2,3,", sep: "", expected: []string{",", "1", ",", ",", "2", ",", "3", ","}},
		{s: ",1,,2,3,", sep: ",,", expected: []string{",1", "2,3,"}},
		{s: "1,2,3", sep: "-", expected: []string{"1,2,3"}},
		{s: "", sep: "-", expected: notAssign},
	}

	for _, c := range cases {
		assert.Equal(t, c.expected, SplitExceptEmptyItems(c.s, c.sep))
	}
}
