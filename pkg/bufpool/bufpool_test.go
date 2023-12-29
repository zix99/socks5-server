package bufpool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBufPool(t *testing.T) {
	p := New(128, 5)

	assert.Len(t, p.pool, 0)

	items := [][]byte{
		p.Get(),
		p.Get(),
		p.Get(),
		p.Get(),
		p.Get(),
		p.Get(),
	}
	assert.Len(t, items, 6)
	assert.NotNil(t, items[3])

	added := p.ReturnMany(items...)
	assert.Equal(t, 5, added)
	assert.Len(t, p.pool, 5)

	ng := p.Get()
	assert.NotNil(t, ng)
	assert.Len(t, p.pool, 4)

	assert.True(t, p.Return(ng))
	assert.Len(t, p.pool, 5)
}
