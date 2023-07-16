package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAlphabetNumeric(t *testing.T) {
	count := 1000
	set := make(map[string]struct{}, count)

	for i := 0; i < count; i++ {
		bs := AlphabetNumeric.Generate(100)
		if len(bs) != 100 {
			t.Error("generate error")
			return
		}
		set[string(bs)] = struct{}{}
	}

	assert.Equal(t, count, len(set))
}

func TestNumeric(t *testing.T) {
	count := 1000
	for i := 0; i < count; i++ {
		num := AlphabetNumeric.Intn(100)

		// n >= 0
		assert.GreaterOrEqual(t, num, 0)
		// n <= 100
		assert.LessOrEqual(t, num, 100)
	}
}
