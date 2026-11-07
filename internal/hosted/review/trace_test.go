package review

import (
	"strings"
	"testing"
)

func TestDecodeTraceFileIsPinnedAndBounded(t *testing.T) {
	head := strings.Repeat("c", 40)
	valid := `{"revision":"` + head + `","tier":"plus","spend":{"input_tokens":1,"output_tokens":1,"cost_microusd":1,"model_calls":1,"tool_calls":0},"events":[{"seq":1,"agent":"planner","kind":"model","name":"planner","elapsed_ms":3}]}`
	if trace, err := DecodeTraceFile([]byte(valid), head); err != nil || len(trace.Events) != 1 {
		t.Fatalf("trace=%#v err=%v", trace, err)
	}
	cases := map[string]string{
		"stale":     strings.Replace(valid, head, strings.Repeat("d", 40), 1),
		"base tier": strings.Replace(valid, `"plus"`, `"base"`, 1),
		"bad kind":  strings.Replace(valid, `"kind":"model"`, `"kind":"shell"`, 1),
		"bad seq":   strings.Replace(valid, `"seq":1`, `"seq":2`, 1),
		"unknown":   strings.Replace(valid, `"elapsed_ms":3`, `"elapsed_ms":3,"raw_error":"x"`, 1),
		"long":      strings.Replace(valid, `"name":"planner"`, `"name":"planner","detail":"`+strings.Repeat("x", MaxTraceDetailBytes+1)+`"`, 1),
		"trailing":  valid + "{}",
	}
	for name, body := range cases {
		if _, err := DecodeTraceFile([]byte(body), head); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}
