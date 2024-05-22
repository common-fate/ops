package ops

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

type fooInput struct {
	Bar   string `json:"bar"`
	Other string `json:"other,omitempty"`
}

type example struct {
}

func (example) Metadata() ServiceMetadata {
	return ServiceMetadata{
		ID:          "example",
		DisplayName: "Example",
		Description: "My Example service",
		OperationMetadata: map[string]OperationMetadata{
			"Foo": {
				Description: "does foo",
			},
		},
	}
}

func (e example) Foo(ctx context.Context, input fooInput) string {
	return "hello " + input.Bar
}

func (e *example) Bar(ctx context.Context, input fooInput) string {
	return "hello " + input.Bar
}

type second struct {
}

type secondOutput struct {
	Example string `json:"example"`
}

func (s second) Foo(ctx context.Context, input fooInput) (secondOutput, error) {
	res := secondOutput{
		Example: "hello " + input.Bar,
	}
	return res, nil
}

type pointerOutput struct {
	Example string `json:"example"`
}

func (pointerOutput) Metadata() ServiceMetadata {
	return ServiceMetadata{
		ID: "pointerOutput",
	}
}

func (s *pointerOutput) Foo(ctx context.Context, input fooInput) (secondOutput, error) {
	res := secondOutput{
		Example: "hello " + input.Bar,
	}
	return res, nil
}

func TestServiceDefsSnapshot(t *testing.T) {
	o := New()
	o.Register(&example{})
	h, err := o.Build()
	if err != nil {
		t.Fatal(err)
	}
	got := h.ServiceDefinitions()

	snaps.MatchJSON(t, got)
}

func TestCallService(t *testing.T) {
	ctx := context.Background()
	o := New()
	o.Register(&example{})
	h, err := o.Build()
	if err != nil {
		t.Fatal(err)
	}

	got, err := h.Call(ctx, "example", "Foo", json.RawMessage(`{"bar": "testing"}`))
	if err != nil {
		t.Fatal(err)
	}

	want := `"hello testing"`

	assert.Equal(t, want, string(got))
}

func TestCallSecond(t *testing.T) {
	ctx := context.Background()
	o := New()
	o.Register(&second{})
	h, err := o.Build()
	if err != nil {
		t.Fatal(err)
	}

	got, err := h.Call(ctx, "second", "Foo", json.RawMessage(`{"bar": "testing"}`))
	if err != nil {
		t.Fatal(err)
	}

	want := `{"example":"hello testing"}`

	assert.Equal(t, want, string(got))
}

func TestCallPointer(t *testing.T) {
	ctx := context.Background()
	o := New()
	o.Register(&pointerOutput{})
	h, err := o.Build()
	if err != nil {
		t.Fatal(err)
	}

	got, err := h.Call(ctx, "pointerOutput", "Foo", json.RawMessage(`{"bar": "testing"}`))
	if err != nil {
		t.Fatal(err)
	}

	want := `{"example":"hello testing"}`

	assert.Equal(t, want, string(got))
}

func TestCallWithNoPointerReturnsError(t *testing.T) {
	o := New()
	o.Register(pointerOutput{})
	_, err := o.Build()
	assert.Error(t, err)
}
