package kv_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoderWithMap(t *testing.T) {
	t.Run("StringMap", func(t *testing.T) {
		var buf strings.Builder
		enc := kv.NewEncoder(&buf)
		input := map[string]string{"key1": "value1", "key2": "value2"}
		require.NoError(t, enc.Encode(input))
		// Map iteration order is random; decode back and compare
		result := make(map[string]string)
		require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(result))
		assert.Equal(t, input, result)
	})

	t.Run("CustomDelimiters", func(t *testing.T) {
		var buf strings.Builder
		enc := kv.NewEncoder(&buf)
		enc.FieldDelimiter(':')
		enc.RowDelimiter(';')
		require.NoError(t, enc.Encode(map[string]string{"k": "v"}))
		assert.Equal(t, "k:v;", buf.String())
		// round-trip: decoder must also use matching delimiters
		result := make(map[string]string)
		dec := kv.NewDecoder(strings.NewReader(buf.String()))
		dec.FieldDelimiter(':')
		dec.RowDelimiter(';')
		require.NoError(t, dec.Decode(result))
		assert.Equal(t, map[string]string{"k": "v"}, result)
	})
}

func TestEncoderWithStruct(t *testing.T) {
	t.Run("WithTags", func(t *testing.T) {
		type testStruct struct {
			Key1 string `kv:"key1"`
			Key2 int    `kv:"key2"`
			Key3 bool   `kv:"key3"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Key1: "hello", Key2: 42, Key3: true}))
		assert.Equal(t, "key1=hello\nkey2=42\nkey3=true\n", buf.String())
	})

	t.Run("WithoutTags", func(t *testing.T) {
		type testStruct struct {
			Alpha string
			Beta  int
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Alpha: "a", Beta: 1}))
		assert.Equal(t, "Alpha=a\nBeta=1\n", buf.String())
	})

	t.Run("IgnoreTag", func(t *testing.T) {
		type testStruct struct {
			Keep   string `kv:"keep"`
			Hidden string `kv:"-"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Keep: "yes", Hidden: "no"}))
		assert.Equal(t, "keep=yes\n", buf.String())
	})

	t.Run("Omitempty", func(t *testing.T) {
		type testStruct struct {
			Present string  `kv:"present"`
			Empty   string  `kv:"empty,omitempty"`
			NilPtr  *string `kv:"nilptr,omitempty"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Present: "here"}))
		assert.Equal(t, "present=here\n", buf.String())
	})

	t.Run("SliceWithDelim", func(t *testing.T) {
		type testStruct struct {
			Items []string `kv:"items,delim:/"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Items: []string{"a", "b", "c"}}))
		assert.Equal(t, "items=a/b/c\n", buf.String())
	})

	t.Run("SliceDefaultDelim", func(t *testing.T) {
		type testStruct struct {
			Tags []string `kv:"tags"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Tags: []string{"x", "y"}}))
		assert.Equal(t, "tags=x,y\n", buf.String())
	})

	t.Run("Pointer", func(t *testing.T) {
		type testStruct struct {
			Val    *string `kv:"val"`
			Absent *string `kv:"absent"`
		}
		s := "pointed"
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Val: &s}))
		assert.Equal(t, "val=pointed\nabsent=\n", buf.String())
	})

	t.Run("CatchAll", func(t *testing.T) {
		type testStruct struct {
			Name  string            `kv:"name"`
			Extra map[string]string `kv:"*"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{
			Name:  "foo",
			Extra: map[string]string{"x": "1"},
		}))
		result := make(map[string]string)
		require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(result))
		assert.Equal(t, map[string]string{"name": "foo", "x": "1"}, result)
	})

	t.Run("PointerToStruct", func(t *testing.T) {
		type testStruct struct {
			Val string `kv:"val"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(&testStruct{Val: "ptr"}))
		assert.Equal(t, "val=ptr\n", buf.String())
	})
}

func TestEncoderFloatPrecision(t *testing.T) {
	type testStruct struct {
		F64 float64 `kv:"f64"`
		F32 float32 `kv:"f32"`
	}
	original := testStruct{F64: 1.0000000000000002, F32: 1.0000001}
	var buf strings.Builder
	require.NoError(t, kv.NewEncoder(&buf).Encode(original))
	var decoded testStruct
	require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(&decoded))
	assert.Equal(t, original.F64, decoded.F64)
	assert.Equal(t, original.F32, decoded.F32)
}

func TestEncoderRoundtrip(t *testing.T) {
	type config struct {
		Host    string            `kv:"host"`
		Port    int               `kv:"port"`
		Enabled bool              `kv:"enabled"`
		Tags    []string          `kv:"tags,delim:/"`
		Extra   map[string]string `kv:"*"`
	}
	original := config{
		Host:    "10.0.0.1",
		Port:    8080,
		Enabled: true,
		Tags:    []string{"a", "b"},
		Extra:   map[string]string{"region": "eu"},
	}

	var buf strings.Builder
	require.NoError(t, kv.NewEncoder(&buf).Encode(original))

	var decoded config
	require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(&decoded))
	assert.Equal(t, original, decoded)
}

func TestEncoderQuoting(t *testing.T) {
	t.Run("ValueWithWhitespace", func(t *testing.T) {
		type testStruct struct {
			Val string `kv:"val"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Val: "  hello  "}))
		var decoded testStruct
		require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(&decoded))
		assert.Equal(t, "  hello  ", decoded.Val)
	})

	t.Run("KeyWithCommentPrefix", func(t *testing.T) {
		type testStruct struct {
			Val string `kv:"#commented"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Val: "x"}))
		// key must be quoted so decoder doesn't skip it as a comment
		var decoded testStruct
		require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(&decoded))
		assert.Equal(t, "x", decoded.Val)
	})

	t.Run("ValueWithRowDelimiterErrors", func(t *testing.T) {
		type testStruct struct {
			Val string `kv:"val"`
		}
		var buf strings.Builder
		err := kv.NewEncoder(&buf).Encode(testStruct{Val: "line1\nline2"})
		require.Error(t, err)
	})

	t.Run("ValueWithDelimiter", func(t *testing.T) {
		type testStruct struct {
			Val string `kv:"val"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Val: "a=b"}))
		assert.Equal(t, "val=\"a=b\"\n", buf.String())
		// round-trip
		var decoded testStruct
		require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(&decoded))
		assert.Equal(t, "a=b", decoded.Val)
	})

	t.Run("ValueWithSingleQuote", func(t *testing.T) {
		type testStruct struct {
			Val string `kv:"val"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Val: "it's"}))
		var decoded testStruct
		require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(&decoded))
		assert.Equal(t, "it's", decoded.Val)
	})

	t.Run("ValueWithBackslash", func(t *testing.T) {
		type testStruct struct {
			Val string `kv:"val"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(testStruct{Val: `a\b`}))
		var decoded testStruct
		require.NoError(t, kv.NewDecoder(strings.NewReader(buf.String())).Decode(&decoded))
		assert.Equal(t, `a\b`, decoded.Val)
	})
}

func TestEncoderErrors(t *testing.T) {
	t.Run("NilPointer", func(t *testing.T) {
		var buf strings.Builder
		var p *struct{ V string }
		err := kv.NewEncoder(&buf).Encode(p)
		require.Error(t, err)
	})

	t.Run("UnsupportedType", func(t *testing.T) {
		var buf strings.Builder
		err := kv.NewEncoder(&buf).Encode(42)
		require.Error(t, err)
	})

	t.Run("EmptyKey", func(t *testing.T) {
		var buf strings.Builder
		err := kv.NewEncoder(&buf).Encode(map[string]string{"": "value"})
		require.Error(t, err)
	})

	t.Run("MultipleCatchAll", func(t *testing.T) {
		type testStruct struct {
			A map[string]string `kv:"*"`
			B map[string]string `kv:"*"`
		}
		var buf strings.Builder
		err := kv.NewEncoder(&buf).Encode(testStruct{
			A: map[string]string{"a": "1"},
			B: map[string]string{"b": "2"},
		})
		require.ErrorIs(t, err, kv.ErrInvalidTags)
	})

	t.Run("NonMapCatchAll", func(t *testing.T) {
		type testStruct struct {
			A string `kv:"*"`
		}
		var buf strings.Builder
		err := kv.NewEncoder(&buf).Encode(testStruct{A: "foo"})
		require.ErrorIs(t, err, kv.ErrInvalidTags)
	})

	t.Run("UnexportedFieldWithTag", func(t *testing.T) {
		type testStruct struct {
			Exported   string `kv:"ok"`
			unexported string `kv:"bad"` //nolint:govet
		}
		var buf strings.Builder
		err := kv.NewEncoder(&buf).Encode(testStruct{Exported: "x"})
		require.ErrorIs(t, err, kv.ErrInvalidTags)
	})
}

func TestEncoderTextMarshaler(t *testing.T) {
	t.Run("ValueReceiver", func(t *testing.T) {
		type withMarshaler struct {
			Val encodingTextValue `kv:"val"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(withMarshaler{Val: encodingTextValue("hello")}))
		assert.Equal(t, "val=HELLO\n", buf.String())
	})

	t.Run("PointerReceiver", func(t *testing.T) {
		type withMarshaler struct {
			Val ptrReceiverValue `kv:"val"`
		}
		var buf strings.Builder
		require.NoError(t, kv.NewEncoder(&buf).Encode(withMarshaler{Val: ptrReceiverValue("hello")}))
		assert.Equal(t, "val=HELLO\n", buf.String())
	})
}

// encodingTextValue uppercases its content when marshaled (value receiver).
type encodingTextValue string

func (v encodingTextValue) MarshalText() ([]byte, error) {
	return []byte(strings.ToUpper(string(v))), nil
}

// ptrReceiverValue uppercases its content when marshaled (pointer receiver).
type ptrReceiverValue string

func (v *ptrReceiverValue) MarshalText() ([]byte, error) {
	return []byte(strings.ToUpper(string(*v))), nil
}

func ExampleEncoder() {
	type Config struct {
		Host    string   `kv:"host"`
		Port    int      `kv:"port"`
		Tags    []string `kv:"tags,delim:/"`
		Comment string   `kv:"-"`
	}
	cfg := Config{Host: "localhost", Port: 9000, Tags: []string{"a", "b"}, Comment: "ignored"}
	var buf strings.Builder
	if err := kv.NewEncoder(&buf).Encode(cfg); err != nil {
		panic(err)
	}
	fmt.Print(buf.String())
	// Output:
	// host=localhost
	// port=9000
	// tags=a/b
}
