package wasm

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func v5HostModule() []byte {
	code := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	code = append(code, v5Section(1, []byte{2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 2, 0x7f, 0x7f, 1, 0x7e})...)
	imports := append(append([]byte{1}, v5Name("campusos")...), v5Name("host_call")...)
	code = append(code, v5Section(2, append(imports, 0, 0))...)
	code = append(code, v5Section(3, []byte{1, 1})...)
	code = append(code, v5Section(5, []byte{1, 0, 2})...)
	exports := append(append([]byte{2}, v5Name("memory")...), 2, 0)
	exports = append(append(exports, v5Name("campusos_invoke")...), 0, 1)
	code = append(code, v5Section(7, exports)...)
	body := []byte{0, 0x42}
	body = append(body, v5SLEB(int64(uint64(65536)<<32))...)
	body = append(body, 0x20, 0, 0x20, 1, 0x41, 0x80, 0x80, 4, 0x41, 0x80, 0x80, 4, 0x10, 0, 0xad, 0x84, 0x0b)
	return append(code, v5Section(10, append(append([]byte{1}, v5ULEB(uint64(len(body)))...), body...))...)
}

func TestV5WasmTypedHostImport(t *testing.T) {
	count := 0
	callback := func(_ context.Context, code string, raw json.RawMessage) (json.RawMessage, error) {
		count++
		if code != "community.thread.public.read" || string(raw) != `{"thread_id":"42"}` {
			t.Fatal("authority/payload changed", code, string(raw))
		}
		return json.RawMessage(`{"thread_id":"42","title":"public"}`), nil
	}
	s, err := newV5Sandbox(context.Background(), v5HostModule(), V5SandboxLimits{1, 1, 500}, time.Now().Add(time.Minute), map[string]bool{"community.thread.public.read": true}, callback)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if err := s.Check(context.Background()); err != nil || count != 0 {
		t.Fatal("readiness performed Host IO", err)
	}
	out, err := s.Invoke(context.Background(), []byte(`{"capability":"community.thread.public.read","payload":{"thread_id":"42"}}`), 1024)
	if err != nil || count != 1 || string(out) != `{"thread_id":"42","title":"public"}` {
		t.Fatal(string(out), err, count)
	}
	for _, raw := range []string{
		`{"capability":"secret.system.read","payload":{}}`,
		`{"capability":"community.thread.public.read","capability":"community.thread.public.read","payload":{}}`,
		`{"capability":"community.thread.public.read","payload":{"thread_id":"42"},"owner":"1002"}`,
	} {
		if out, err := s.Invoke(context.Background(), []byte(raw), 1024); err == nil || out != nil {
			t.Fatal("untrusted Host request accepted", string(out), err)
		}
	}
	if count != 1 {
		t.Fatal("undeclared/malformed request reached broker")
	}
}
