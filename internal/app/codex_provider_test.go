package app

import "testing"

func TestCodexSSEChunkReadsNestedFunctionCallItem(t *testing.T) {
	chunk, ok := codexSSEChunk([]byte(`{
		"type":"response.output_item.added",
		"output_index":2,
		"item":{"id":"item_1","call_id":"call_1","name":"read","arguments":"{\"path\":\"README.md\"}"}
	}`))
	if !ok || len(chunk.ToolCallDeltas) != 1 {
		t.Fatalf("chunk = %#v, ok = %v", chunk, ok)
	}
	delta := chunk.ToolCallDeltas[0]
	if delta.ID != "call_1" || delta.Name != "read" || delta.ArgsDelta == "" || delta.Index != 2 {
		t.Fatalf("delta = %#v", delta)
	}
}
