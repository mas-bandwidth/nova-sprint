package card

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultReadNeeds(t *testing.T) {
	t.Parallel()
	n := DefaultReadNeeds()
	assert.Empty(t, n.Tags)
	assert.Equal(t, map[string]int{"cpu": 1, "memory": 1}, n.Amounts)
}

func TestParseBriefNeeds(t *testing.T) {
	t.Parallel()

	t.Run("ValidResources", func(t *testing.T) {
		t.Parallel()
		brief := "pro\nRESOURCES: cpu 4, memory 8, mac, ps5\n\nTask description"
		raw, count, err := ParseBriefNeeds(brief)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.Equal(t, "cpu 4, memory 8, mac, ps5", raw)
	})

	t.Run("NoResources", func(t *testing.T) {
		t.Parallel()
		brief := "pro\nPATHS: a, b\n\nTask description"
		raw, count, err := ParseBriefNeeds(brief)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		assert.Empty(t, raw)
	})

	t.Run("DuplicateResources", func(t *testing.T) {
		t.Parallel()
		brief := "pro\nRESOURCES: cpu 2\nRESOURCES: cpu 4\n\nTask description"
		_, count, err := ParseBriefNeeds(brief)
		require.Error(t, err)
		assert.Equal(t, 2, count)
		assert.Contains(t, err.Error(), "duplicate RESOURCES")
	})

	t.Run("RefuseNeedsRoute", func(t *testing.T) {
		t.Parallel()
		brief := "pro\nNEEDS-ROUTE: ps5\n\nTask description"
		_, _, err := ParseBriefNeeds(brief)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "NEEDS-ROUTE")
		assert.Contains(t, err.Error(), "retired in v3")
		assert.Contains(t, err.Error(), "RESOURCES")
	})
}

func TestParseNeeds(t *testing.T) {
	t.Parallel()

	tags := map[string]bool{"mac": true, "linux": true, "gpu": true}
	resources := map[string]bool{"ps5": true, "metal": true}

	t.Run("DefaultsWhenEmpty", func(t *testing.T) {
		t.Parallel()
		n, err := ParseNeeds("", tags, resources)
		require.NoError(t, err)
		assert.Empty(t, n.Tags)
		assert.Equal(t, map[string]int{"cpu": 1, "memory": 1}, n.Amounts)
	})

	t.Run("FullSpec", func(t *testing.T) {
		t.Parallel()
		n, err := ParseNeeds("cpu 4, memory 8, mac, ps5", tags, resources)
		require.NoError(t, err)
		assert.Equal(t, []string{"mac"}, n.Tags)
		assert.Equal(t, map[string]int{"cpu": 4, "memory": 8, "ps5": 1}, n.Amounts)
	})

	t.Run("BareResourceGetsOne", func(t *testing.T) {
		t.Parallel()
		n, err := ParseNeeds("ps5, metal 2", tags, resources)
		require.NoError(t, err)
		assert.Empty(t, n.Tags)
		assert.Equal(t, map[string]int{"cpu": 1, "memory": 1, "ps5": 1, "metal": 2}, n.Amounts)
	})

	t.Run("BareTagGetsNoAmount", func(t *testing.T) {
		t.Parallel()
		n, err := ParseNeeds("gpu, linux", tags, resources)
		require.NoError(t, err)
		assert.Equal(t, []string{"gpu", "linux"}, n.Tags)
		assert.Equal(t, map[string]int{"cpu": 1, "memory": 1}, n.Amounts)
	})

	t.Run("TagWithAmountRefused", func(t *testing.T) {
		t.Parallel()
		_, err := ParseNeeds("gpu 2", tags, resources)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot have an amount")
	})

	t.Run("ZeroOrNegativeAmountRefused", func(t *testing.T) {
		t.Parallel()
		_, err := ParseNeeds("cpu 0", tags, resources)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least 1")

		_, err = ParseNeeds("memory -2", tags, resources)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least 1")
	})

	t.Run("UnknownNameRefused", func(t *testing.T) {
		t.Parallel()
		_, err := ParseNeeds("unknown-thing", tags, resources)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown tag or resource")
	})
}

func TestCardNeeds(t *testing.T) {
	t.Parallel()

	brief := "flash\nRESOURCES: cpu 8, memory 16, linux, gpu\n\nTask body"
	n := CardNeeds(brief)
	assert.Equal(t, []string{"gpu", "linux"}, n.Tags)
	assert.Equal(t, map[string]int{"cpu": 8, "memory": 16}, n.Amounts)
}

func TestResolveNeeds(t *testing.T) {
	t.Parallel()

	tags := map[string]bool{"linux": true}
	resources := map[string]bool{"ps5": true}

	t.Run("ReadAlwaysDefault", func(t *testing.T) {
		t.Parallel()
		brief := "pro\nRESOURCES: cpu 8, memory 32, linux, ps5\n\nRead task"
		n := ResolveNeeds("read", brief, tags, resources)
		assert.Empty(t, n.Tags)
		assert.Equal(t, map[string]int{"cpu": 1, "memory": 1}, n.Amounts)
	})

	t.Run("WorkResolves", func(t *testing.T) {
		t.Parallel()
		brief := "pro\nRESOURCES: cpu 4, memory 8, linux, ps5\n\nWork task"
		n := ResolveNeeds("work", brief, tags, resources)
		assert.Equal(t, []string{"linux"}, n.Tags)
		assert.Equal(t, map[string]int{"cpu": 4, "memory": 8, "ps5": 1}, n.Amounts)
	})
}
