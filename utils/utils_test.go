package utils

import (
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCheckErr(t *testing.T) {
	cases := []struct {
		param       error
		expectFatal bool
	}{
		{
			param:       nil,
			expectFatal: false,
		}, {
			param:       errors.New("error"),
			expectFatal: true,
		},
	}

	defer func() { log.StandardLogger().ExitFunc = nil }()

	var occurredFatal bool
	log.StandardLogger().ExitFunc = func(int) { occurredFatal = true }

	assert := assert.New(t)
	for _, c := range cases {
		occurredFatal = false
		CheckErr(c.param)
		assert.Equal(c.expectFatal, occurredFatal)
	}
}

func TestToSnakeCase(t *testing.T) {
	assert := assert.New(t)

	assert.Equal("my", ToSnakeCase("My"))
	assert.Equal("123", ToSnakeCase("123"))
	assert.Equal("123abc", ToSnakeCase("123abc"))
	assert.Equal("123abc_def", ToSnakeCase("123abcDef"))
	assert.Equal("123abc_def_ghi", ToSnakeCase("123abcDefGHI"))
	assert.Equal("123abc_def_gh_ij", ToSnakeCase("123abcDefGHIj"))
	assert.Equal("123abc_def_gh_ij_k", ToSnakeCase("123abcDefGHIjK"))
	assert.Equal("my_name_is_tom", ToSnakeCase("MyNameIsTom"))
	assert.Equal("my_name_is_tom", ToSnakeCase("myNameIsTom"))
	assert.Equal(" my_name_is_tom ", ToSnakeCase(" myNameIsTom "))
	assert.Equal(" my_name_is_tom  your_name_is_b", ToSnakeCase(" myNameIsTom  yourNameIsB"))
}

func TestContains(t *testing.T) {
	assert := assert.New(t)

	lst := []string{"A1", "B1", "C1"}
	assert.False(Contains(lst, ""))
	assert.True(Contains(lst, "A1"))
	assert.False(Contains(lst, "a1"))
	assert.False(Contains(lst, "A2"))
	assert.False(Contains(lst, "A1 "))
}

func TestTrim(t *testing.T) {
	assert := assert.New(t)

	assert.Equal("테스트", Trim("테스트"))
	assert.Equal("테스트", Trim("   테스트   "))
	assert.Equal("하나 공백", Trim("   하나 공백   "))
	assert.Equal("다수 공백", Trim("   다수    공백   "))
	assert.Equal("다수 공백 여러개", Trim("   다수    공백   여러개   "))
	assert.Equal("@ 특수문자 $", Trim("   @    특수문자   $   "))

	// 다수의 라인이 포함되어 있는 문자열 체크
	assert.Equal("라인 1 라인2 라인3 라인4 라인5", Trim(`

		라인    1
		라인2


		라인3

		라인4


		라인5

		`))
}

func TestTrimMultiLine(t *testing.T) {
	assert := assert.New(t)

	assert.Equal("", TrimMultiLine(""))
	assert.Equal("", TrimMultiLine("   "))
	assert.Equal("a", TrimMultiLine("  a  "))

	assert.Equal("라인 1\r\n라인2\r\n\r\n라인3\r\n\r\n라인4\r\n\r\n라인5", TrimMultiLine(`

		라인    1
		라인2


		라인3

		라인4



		라인5


		`))

	assert.Equal("라인 1\r\n\r\n라인2\r\n\r\n라인3\r\n라인4\r\n라인5", TrimMultiLine(` 라인    1


		라인2


		라인3
		라인4
		라인5   `))

	assert.Equal("", TrimMultiLine(`

		


		`))

	assert.Equal("1", TrimMultiLine(`

		1


		`))
}

func TestFormatCommas(t *testing.T) {
	assert := assert.New(t)

	assert.Equal("0", FormatCommas(0))
	assert.Equal("100", FormatCommas(100))
	assert.Equal("1,000", FormatCommas(1000))
	assert.Equal("1,234,567", FormatCommas(1234567))
	assert.Equal("-1,234,567", FormatCommas(-1234567))
}
