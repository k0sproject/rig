package kv_test

import (
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecoderWithMap(t *testing.T) {
	t.Run("Simple", func(t *testing.T) {
		target := make(map[string]string)
		data := "key1=value1\nkey2=value"
		decoder := kv.NewDecoder(strings.NewReader(data))
		require.NoError(t, decoder.Decode(target))
		assert.Len(t, target, 2)
		v, ok := target["key1"]
		assert.True(t, ok)
		assert.Equal(t, "value1", v)
		v, ok = target["key2"]
		assert.True(t, ok)
		assert.Equal(t, "value", v)
	})
	t.Run("Quoted", func(t *testing.T) {
		target := make(map[string]string)
		data := "key1=\"value with spaces\"\nkey2='value with spaces'"
		decoder := kv.NewDecoder(strings.NewReader(data))
		require.NoError(t, decoder.Decode(target))
		assert.Len(t, target, 2)
		v, ok := target["key1"]
		assert.True(t, ok)
		assert.Equal(t, "value with spaces", v)
		v, ok = target["key2"]
		assert.True(t, ok)
		assert.Equal(t, "value with spaces", v)
	})
	t.Run("Nested Quoted", func(t *testing.T) {
		target := make(map[string]string)
		data := "key1=\"value with \\\"quotes\\\"\"\nkey2='value with \"quotes\"'"
		decoder := kv.NewDecoder(strings.NewReader(data))
		require.NoError(t, decoder.Decode(target))
		assert.Len(t, target, 2)
		v, ok := target["key1"]
		assert.True(t, ok)
		assert.Equal(t, "value with \"quotes\"", v)
		v, ok = target["key2"]
		assert.True(t, ok)
		assert.Equal(t, "value with \"quotes\"", v)
	})
	t.Run("Mismatched quotes", func(t *testing.T) {
		target := make(map[string]string)
		data := "key1=key1key2='value with \"quotes\""
		decoder := kv.NewDecoder(strings.NewReader(data))
		require.ErrorContains(t, decoder.Decode(target), "mismatch")
	})
}

func TestDecoderWithStruct(t *testing.T) {
	t.Run("With tags", func(t *testing.T) {
		t.Run("Simple", func(t *testing.T) {
			type testStruct struct {
				Key1 string `kv:"key1"`
				Key2 string `kv:"key2"`
				Key3 bool   `kv:"key3"`
				Key4 int    `kv:"key4"`
			}
			target := testStruct{}
			data := "key1=value1\nkey2=value2\nkey3=true\nkey4=42"
			decoder := kv.NewDecoder(strings.NewReader(data))
			require.NoError(t, decoder.Decode(&target))
			assert.Equal(t, "value1", target.Key1)
			assert.Equal(t, "value2", target.Key2)
			assert.True(t, target.Key3)
			assert.Equal(t, 42, target.Key4)
		})
		t.Run("Slices and pointers", func(t *testing.T) {
			type testStruct struct {
				Key1 *string  `kv:"key1"`
				Key2 []string `kv:"key2,delim::"`
			}
			target := testStruct{}
			data := "key1=value1\nkey2=a:b:c:d:e"
			decoder := kv.NewDecoder(strings.NewReader(data))
			require.NoError(t, decoder.Decode(&target))
			assert.NotNil(t, target.Key1)
			assert.Equal(t, "value1", *target.Key1)
			assert.Equal(t, []string{"a", "b", "c", "d", "e"}, target.Key2)
		})
		t.Run("Catch all", func(t *testing.T) {
			type testStruct struct {
				Key1  string            `kv:"key1"`
				Key2  string            `kv:"key2"`
				Extra map[string]string `kv:"*"`
			}
			target := testStruct{}
			data := "key1=value1\nkey2=value2\nkey3=value3"
			decoder := kv.NewDecoder(strings.NewReader(data))
			require.NoError(t, decoder.Decode(&target))
			assert.Equal(t, "value1", target.Key1)
			assert.Equal(t, "value2", target.Key2)
			assert.Len(t, target.Extra, 1)
			v, ok := target.Extra["key3"]
			assert.True(t, ok)
			assert.Equal(t, "value3", v)
		})
	})
	t.Run("Without tags", func(t *testing.T) {
		t.Run("Simple", func(t *testing.T) {
			type testStruct struct {
				Key1 string
				Key2 string
				Key3 bool
			}
			target := testStruct{Key3: true}
			data := "Key1=value1\nKey2=value2\nKey3=false"
			decoder := kv.NewDecoder(strings.NewReader(data))
			require.NoError(t, decoder.Decode(&target))
			assert.Equal(t, "value1", target.Key1)
			assert.Equal(t, "value2", target.Key2)
			assert.False(t, target.Key3)
		})
	})
}
