package session

// E2E 数据种子（测试工程师用）：仅当设置 MINDFS_E2E_SEED_ROOT 等环境变量时运行，
// 向指定 root 的元数据目录写入带标记的会话数据，供备份/恢复(R-5.2)等黑盒
// 验收用例使用。常规 `go test` 下自动跳过，不影响任何现有测试。
//
// 用法示例（Git Bash）：
//   MINDFS_E2E_SEED_ROOT=/e/path/to/root \
//   MINDFS_E2E_SEED_ROOTID=proj-a \
//   MINDFS_E2E_SEED_METALOC=project \
//   go test -run TestE2ESeedSessions ./server/internal/session/ -count=1 -v
//
// home 布局需同时把 USERPROFILE 指向沙箱 home；.link 兜底布局需把 APPDATA
// 指向沙箱配置目录，并预先放好 sessions/session-list.db.link 指针文件。

import (
	"context"
	"fmt"
	"os"
	"testing"

	"mindfs/server/internal/fs"
)

func TestE2ESeedSessions(t *testing.T) {
	rootPath := os.Getenv("MINDFS_E2E_SEED_ROOT")
	rootID := os.Getenv("MINDFS_E2E_SEED_ROOTID")
	if rootPath == "" || rootID == "" {
		t.Skip("MINDFS_E2E_SEED_ROOT / MINDFS_E2E_SEED_ROOTID not set; e2e seed disabled")
	}
	metaLoc := os.Getenv("MINDFS_E2E_SEED_METALOC")
	if metaLoc == "" {
		metaLoc = fs.MetaLocationProject
	}

	root := fs.RootInfo{ID: rootID, Name: rootID, RootPath: rootPath, MetaLocation: metaLoc}
	m := NewManager(root)
	defer m.Shutdown()

	ctx := context.Background()
	const count = 3
	for i := 1; i <= count; i++ {
		s, err := m.Create(ctx, CreateInput{
			Type:  TypeChat,
			Agent: "claude",
			Name:  fmt.Sprintf("seed-%s-%d", rootID, i),
		})
		if err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
		userMsg := fmt.Sprintf("质检种子问题 %d（root=%s）", i, rootID)
		agentMsg := fmt.Sprintf("质检种子回答 %d，唯一标记 SEED-%s-%d", i, rootID, i)
		if err := m.AddExchangeForAgent(ctx, s, "user", userMsg, "claude", "", "", ""); err != nil {
			t.Fatalf("add user exchange %d: %v", i, err)
		}
		if err := m.AddExchangeForAgent(ctx, s, "agent", agentMsg, "claude", "", "", ""); err != nil {
			t.Fatalf("add agent exchange %d: %v", i, err)
		}
		t.Logf("seeded session key=%s name=seed-%s-%d", s.Key, rootID, i)
	}
}
