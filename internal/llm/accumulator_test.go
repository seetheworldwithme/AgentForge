package llm

import (
	"reflect"
	"testing"
)

func TestAccumulatorSingleCall(t *testing.T) {
	acc := newToolCallAccumulator()
	acc.add(deltaChunk{Index: 0, ID: "call_1", FunctionName: "get_system_info", ArgumentsFrag: `{"query":"`})
	acc.add(deltaChunk{Index: 0, ArgumentsFrag: `memory"`})
	acc.add(deltaChunk{Index: 0, ArgumentsFrag: `}`})

	got := acc.result()
	want := []expectedCall{
		{ID: "call_1", Name: "get_system_info", Args: `{"query":"memory"}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAccumulatorMultipleCalls(t *testing.T) {
	acc := newToolCallAccumulator()
	acc.add(deltaChunk{Index: 0, ID: "call_1", FunctionName: "tool_a", ArgumentsFrag: `{}`})
	acc.add(deltaChunk{Index: 1, ID: "call_2", FunctionName: "tool_b", ArgumentsFrag: `{"x":1`})
	acc.add(deltaChunk{Index: 1, ArgumentsFrag: `}`})

	got := acc.result()
	want := []expectedCall{
		{ID: "call_1", Name: "tool_a", Args: `{}`},
		{ID: "call_2", Name: "tool_b", Args: `{"x":1}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAccumulatorEmpty(t *testing.T) {
	acc := newToolCallAccumulator()
	got := acc.result()
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %+v", got)
	}
}
