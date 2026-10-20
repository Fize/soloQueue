package cron

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

func TestWechatCronFailureDiagnosticsPreserveExecutionSuccess(t *testing.T) {
	for _, level := range []string{"L1", "L2"} {
		t.Run(level, func(t *testing.T) {
			store := openTestDB(t)
			task, err := store.CreateTask(context.Background(), CreateTaskInput{Title: "Wechat notification", TaskType: TaskTypeGeneral, Expression: "0 8 * * *", Instruction: "notify", TargetAgent: level, NextRunAt: time.Now().Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			log, err := logger.New(dir, logger.WithConsole(false), logger.WithFile(true))
			if err != nil {
				t.Fatal(err)
			}
			var textCalls, mediaCalls int
			resultSession := &mockSession{hasNotifyChannel: true, askStreamFn: func(context.Context, string) (<-chan iface.AgentEvent, error) {
				events := make(chan iface.AgentEvent, 5)
				events <- &ToolExecStartEvent{CallID: "file", Name: "SendFile", Args: `{}`}
				events <- &ToolExecDoneEvent{CallID: "file", Name: "SendFile", Result: `{"status":"success","file_type":"image","file_name":"test.png","url":"https://example.test/test.png"}`}
				events <- &ToolExecStartEvent{CallID: "submit", Name: "SubmitCronResult", Args: `{"content":"done"}`}
				events <- &ToolExecDoneEvent{CallID: "submit", Name: "SubmitCronResult", Result: `{"content":"done"}`}
				events <- &testDoneEvent{content: "private"}
				close(events)
				return events, nil
			}, sendViaChannelFn: func(context.Context, string) error {
				textCalls++
				return errors.New("wechat_notification_delivery_unknown: network timeout")
			}, sendMediaFn: func(context.Context, []channel.OutboundMedia) error {
				mediaCalls++
				return errors.New("wechat_notification_route_missing: send a WeChat message")
			}}
			manager := &mockSessionManager{session: resultSession, getSession: func(context.Context, string, string) (Session, bool, func(), error) {
				return resultSession, true, func() {}, nil
			}}
			scheduler := NewScheduler(store, manager, log)
			scheduler.retryDelay = 0
			scheduler.SetWorkDir(dir)
			if level == "L1" {
				scheduler.executeL1Task(*task)
			} else {
				scheduler.executeL2Task(*task)
			}
			if textCalls != 1 || mediaCalls != 1 {
				t.Fatalf("text/media calls %d/%d; want 1/1", textCalls, mediaCalls)
			}
			records, err := store.ListExecutionHistory(context.Background(), task.ID, 1, 0)
			if err != nil || len(records) != 1 || records[0].Status != "success" {
				t.Fatalf("notification changed execution result: %v %+v", err, records)
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			var logs strings.Builder
			if err := filepath.WalkDir(filepath.Join(dir, "logs"), func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					raw, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					logs.Write(raw)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs.String(), "wechat_notification_delivery_unknown") || !strings.Contains(logs.String(), "wechat_notification_route_missing") {
				t.Fatal("notification diagnostics missing from existing logs")
			}
		})
	}
}
