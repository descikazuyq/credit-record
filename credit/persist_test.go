package credit

import (
	"os"
	"path/filepath"
	"testing"
)

func buildRoundTripStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "数学", 4)
	mustCourse(t, s, "c2", "物理", 3)
	mustReq(t, s, "s1", "r1", "c1")
	mustReq(t, s, "s1", "r2", "c2")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
	if _, _, err := s.SubmitResult("s1", "e1", Failed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s1", "e2", Passed); err != nil {
		t.Fatal(err)
	}
	// 被拒绝的免修也应留在历史中并持久化。
	if w, _, err := s.ApplyWaiver("s1", "nope", "wbad", "依据"); err != nil || w.Status != WaiverRejected {
		t.Fatalf("构造被拒绝免修失败：%+v %v", w, err)
	}
	if _, _, err := s.ApplyWaiver("s1", "r2", "w1", "竞赛获奖免修"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RevokeWaiver("s1", "w1", "撤销测试"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCourseOpen("c2", false); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	s := buildRoundTripStore(t)
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("Load: existed=%v err=%v", existed, err)
	}
	if loaded.Student("s1") == nil || loaded.Student("s2") == nil {
		t.Fatal("学生未正确恢复")
	}
	if c := loaded.Course("c2"); c == nil || c.Open {
		t.Fatalf("课程停开状态未恢复：%+v", c)
	}
	if e := loaded.Enrollment("s1", "e1"); e == nil || e.Result != Failed {
		t.Fatalf("修读结果未恢复：%+v", e)
	}
	if e := loaded.Enrollment("s1", "e2"); e == nil || e.Result != Passed {
		t.Fatalf("通过结果未恢复：%+v", e)
	}
	if w := loaded.Waiver("s1", "w1"); w == nil || w.Status != WaiverRevoked ||
		w.Basis != "竞赛获奖免修" || w.Reason != "撤销测试" {
		t.Fatalf("撤销免修的依据与状态未恢复：%+v", w)
	}
	if w := loaded.Waiver("s1", "wbad"); w == nil || w.Status != WaiverRejected || w.Reason == "" {
		t.Fatalf("被拒绝免修历史未恢复：%+v", w)
	}

	// 核对结果应与保存前一致：r1 通过计 4 学分，r2 免修撤销且无修读未满足。
	rep := loaded.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("恢复后总学分应为 4，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r2" {
		t.Fatalf("恢复后未满足要求应为 [r2]，得到 %v", rep.Unmet)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w1" {
		t.Fatalf("已撤销免修列表应为 [w1]，得到 %v", rep.RevokedWaivers)
	}
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("被拒绝免修应保留 1 条，得到 %d", len(rep.RejectedWaivers))
	}

	// 再保存一次，内容保持稳定（序号计数器恢复后继续正确分配）。
	if _, _, err := loaded.SubmitResult("s2", "x", Passed); err == nil {
		t.Fatal("不存在的修读应报错")
	}
	mustReq(t, loaded, "s2", "r9", "c1")
	mustEnroll(t, loaded, "s2", "r9", "2025春", "e9")
	if _, _, err := loaded.SubmitResult("s2", "e9", Passed); err != nil {
		t.Fatalf("恢复后继续提交结果失败：%v", err)
	}
}

func TestLoadMissingStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nope.json") // 目录也不存在
	s, existed, err := Load(path)
	if err != nil || existed || s == nil {
		t.Fatalf("文件不存在应返回空记录且无错误：existed=%v err=%v", existed, err)
	}
	if len(s.Students()) != 0 {
		t.Fatal("应为空记录")
	}
	// Save 应能创建缺失的目录层级之外的文件……这里目录不存在，
	// Save 只创建同目录临时文件，因此先建目录再保存验证空记录可写。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("空记录保存失败：%v", err)
	}
}

func TestLoadCorruptAndUnreadableNotOverwritten(t *testing.T) {
	dir := t.TempDir()

	cases := map[string]string{
		"非法JSON": "{ not json",
		"空文件":    "",
		"悬空学生引用": `{"version":1,"students":[{"id":"s1"}],"requirements":[` +
			`{"id":"r1","student":"sX","course":"c1"}]}` +
			"\n",
		"学分为零": `{"version":1,"courses":[{"id":"c1","name":"数学","credit":0,"open":true}]}` + "\n",
		"坏版本":  `{"version":99}` + "\n",
		"多余字段": `{"version":1,"bogus":1}` + "\n",
		"重复学生": `{"version":1,"students":[{"id":"s1"},{"id":"s1"}]}` + "\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			s, existed, err := Load(path)
			if err == nil {
				t.Fatalf("%s：损坏文件应报错，得到 store=%v", name, s)
			}
			if !existed {
				t.Fatalf("%s：损坏文件应标记为已存在", name)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != content {
				t.Fatalf("%s：Load 不得改写损坏文件", name)
			}
		})
	}

	// 无法读取（无权限）时也应报错。
	path := filepath.Join(dir, "noread.json")
	if err := os.WriteFile(path, []byte(`{"version":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, _, err := Load(path); err == nil {
		// root 用户可绕过权限；测试环境若以 root 运行则此断言不适用。
		if os.Getuid() != 0 {
			t.Fatal("不可读文件应报错")
		}
	}
}

func TestSaveDoesNotPartiallyOverwriteOnReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")
	s := buildRoundTripStore(t)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 重新打开再不改任何内容直接保存，文件仍应可再次正常加载。
	s2, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("重新打开失败：existed=%v err=%v", existed, err)
	}
	if err := s2.Save(path); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("无变更重复保存应产生相同内容")
	}
	if _, _, err := Load(path); err != nil {
		t.Fatalf("重复保存后文件损坏：%v", err)
	}
}

func TestSaveRejectsBadPath(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	// 目标路径的父目录不存在：应明确报错而不是静默丢失数据。
	err := s.Save(filepath.Join(t.TempDir(), "no-such-dir", "r.json"))
	if err == nil {
		t.Fatal("向不存在目录保存应报错")
	}
}
