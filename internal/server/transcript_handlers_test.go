package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

func TestSessionTranscriptMergesPersistedAndLiveItems(t *testing.T) {
	workDir := t.TempDir()
	id := fmt.Sprintf("tx%d", time.Now().UnixNano())
	dir := filepath.Join(workDir, "logs", "timelines", "l2-"+id)
	tl, err := timeline.NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = tl.AppendMessage(&timeline.MessagePayload{Role: "user", Content: "legacy question"})
	_ = tl.AppendMessage(&timeline.MessagePayload{Role: "user", Content: "q1", RequestID: "r1", ItemID: "in:r1", Seq: 4})
	_ = tl.AppendMessage(&timeline.MessagePayload{Role: "assistant", Content: "a1", RequestID: "r1", ItemID: "out:r1:0", Seq: 5})
	_ = tl.Close()

	reg := transcript.NewRegistry(transcript.DefaultConfig())
	j := reg.Journal("l2:"+id, dir)
	j.Publish(transcript.Frame{Kind: transcript.KindInputCommitted, RequestID: "r2", ItemID: "in:r2", Text: "q2"})
	j.Publish(transcript.Frame{Kind: transcript.KindItemDelta, RequestID: "r2", ItemID: "out:r2:0", Channel: transcript.ChannelContent, Text: "streaming"})
	j.Publish(transcript.Frame{Kind: transcript.KindInputQueued, RequestID: "r3", Text: "q3"})

	m := NewMux(workDir, nil, WithTranscriptRegistry(reg))
	get := func(query string) transcriptResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, newLocalhostRequest(http.MethodGet, "/api/session/transcript?"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var resp transcriptResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}
	ids := func(items []transcript.Item) string {
		out := make([]string, len(items))
		for i, item := range items {
			out[i] = item.ID
		}
		return strings.Join(out, ",")
	}

	resp := get("session_id=l2:" + id)
	if resp.SessionID != "l2:"+id || resp.Epoch != j.Epoch() || resp.Seq != j.Head() || resp.Seq != 8 {
		t.Fatalf("watermark = %#v (head %d)", resp, j.Head())
	}
	if got := ids(resp.Items); got != "legacy:0,in:r1,out:r1:0,in:r2,out:r2:0" {
		t.Fatalf("items = %s", got)
	}
	if len(resp.Pending) != 1 || resp.Pending[0].RequestID != "r3" {
		t.Fatalf("pending = %#v", resp.Pending)
	}

	page := get("session_id=l2:" + id + "&limit=2")
	if ids(page.Items) != "in:r2,out:r2:0" || !page.HasMore || page.Cursor != "in:r2" {
		t.Fatalf("first page = %s more=%v cursor=%q", ids(page.Items), page.HasMore, page.Cursor)
	}
	older := get("session_id=l2:" + id + "&limit=2&before=" + page.Cursor)
	if ids(older.Items) != "in:r1,out:r1:0" || !older.HasMore {
		t.Fatalf("older page = %s more=%v", ids(older.Items), older.HasMore)
	}

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, newLocalhostRequest(http.MethodGet, "/api/session/transcript", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing session_id status = %d", rec.Code)
	}
}
