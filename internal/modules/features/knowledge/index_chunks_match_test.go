package knowledge

import (
	"strings"
	"testing"
)

func TestIndexCurrentChunksRequireExactRunePartition(t *testing.T) {
	body := strings.Repeat("中", 1023) + "边界内容" + strings.Repeat("a", 1100)
	if !indexChunksMatch(body, indexChunks(body)) {
		t.Fatal("canonical chunks rejected")
	}
	for _, chunks := range [][]string{{body}, {body[:3], body[3:]}, {body[:1024], body[1024:]}, {""}, nil, append(indexChunks(body), "")} {
		if indexChunksMatch(body, chunks) {
			t.Fatal("noncanonical partition accepted")
		}
	}
	changed := indexChunks(body)
	changed[0] = "x" + changed[0][1:]
	if indexChunksMatch(body, changed) {
		t.Fatal("different original bytes accepted")
	}
}
