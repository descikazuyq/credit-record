package credit

import (
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从 Store 层回归“一份免修撤销后，用户换用新编号重新申请同一项要求”
// 的行为，并覆盖保存后重新加载的一致性。撤销取消的只是原申请对要求的满足
// 作用，原申请仍留在历史里；新申请能否生效只看这项要求当前有没有有效免修，
// 不能因为历史里出现过免修就一直拒绝：
//   - 原免修撤销且无通过修读时核对为 0 学分、要求未满足；用该学生名下未
//     使用过的新编号与含实际文字的依据申请应正常生效，核对随之变为已满足、
//     4 学分、来源指向新编号；旧申请仍为已撤销，原要求、原依据与撤销原因
//     保留，两份历史互不覆盖，学分不能因两份申请记成两份；
//   - 旧编号按原要求、原依据重试：新申请之前与新申请已经有效之后都只返回
//     原申请（已撤销），不新增历史、不重新授予学分，也不能替换或撤销已经
//     有效的新申请；旧编号携带不同依据时沿用内容冲突拒绝规则，不能被当作
//     一次新申请；
//   - 新申请已经有效时再用另一个新编号申请同一要求：拒绝并把申请内容与
//     “现有有效免修编号”的原因保留在该学生的拒绝历史中，原有效申请继续
//     满足要求，总学分仍为 4。

const (
	reapplyOldBasis   = "学科竞赛获奖"
	reapplyNewBasis   = "外校同层次课程成绩单"
	reapplyThirdBasis = "高水平运动队证明"
	reapplyReason     = "获奖材料无法核实"
)

// setupRevokedReapplyStore 建立主情形：s1 的 r1 指向 4 学分课程 c1，没有任何
// 修读；w1 曾有效后被撤销，原依据与撤销原因保留。返回的 store 核对必须为
// 0 学分、r1 未满足。
func setupRevokedReapplyStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	if _, a, err := s.ApplyWaiver("s1", "r1", "w1", reapplyOldBasis); err != nil ||
		a != ActionCreated {
		t.Fatalf("原免修 w1 应正常生效，action=%v err=%v", a, err)
	}
	w, changed, err := s.RevokeWaiver("s1", "w1", reapplyReason)
	if err != nil || !changed || w.Status != WaiverRevoked {
		t.Fatalf("撤销 w1 应成功，changed=%v 得到 %+v err=%v", changed, w, err)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("撤销后无通过修读应回到 0 学分、r1 未满足，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w1" {
		t.Fatalf("核对应列出已撤销的 w1，得到 %v", rep.RevokedWaivers)
	}
	return s
}

// assertRevokedW1Untouched 断言旧申请 w1 仍是已撤销，要求、依据与撤销原因
// 均为原值。
func assertRevokedW1Untouched(t *testing.T, s *Store) {
	t.Helper()
	w := s.Waiver("s1", "w1")
	if w == nil || w.Status != WaiverRevoked || w.ReqID != "r1" ||
		w.Basis != reapplyOldBasis || w.Reason != reapplyReason {
		t.Fatalf("旧申请应保持已撤销并保留原要求/依据/原因，得到 %+v", w)
	}
}

// assertW2IsOnlySource 核对：r1 已满足、总学分恰为 4、唯一来源是 w2，
// 旧 w1 仍列为已撤销。
func assertW2IsOnlySource(t *testing.T, s *Store) {
	t.Helper()
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("只能按新免修计一次 4 学分，不能因两份历史计成 8，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 0 {
		t.Fatalf("r1 应由 w2 满足，未满足=%v", rep.Unmet)
	}
	if len(rep.Requirements) != 1 {
		t.Fatalf("应只有一项要求，得到 %d", len(rep.Requirements))
	}
	st := rep.Requirements[0]
	if !st.Satisfied || st.Source != "waiver" || st.WaiverID != "w2" {
		t.Fatalf("来源应是有效免修 w2，已撤销的 w1 不能成为来源，得到 %+v", st)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w1" {
		t.Fatalf("旧申请 w1 应仍列为已撤销，得到 %v", rep.RevokedWaivers)
	}
}

// TestRevokedWaiverAllowsNewIDApplicationAndKeepsHistory 原免修撤销后用新编号
// 申请同一要求应正常生效；新旧两份申请各有编号、依据与状态，旧申请不被
// 覆盖，学分只计一次，保存并重新加载后结论与历史完整保留。
func TestRevokedWaiverAllowsNewIDApplicationAndKeepsHistory(t *testing.T) {
	s := setupRevokedReapplyStore(t)

	w2, a, err := s.ApplyWaiver("s1", "r1", "w2", reapplyNewBasis)
	if err != nil || a != ActionCreated || w2.Status != WaiverApproved {
		t.Fatalf("撤销后用新编号申请应正常生效，得到 %+v action=%v err=%v", w2, a, err)
	}
	assertW2IsOnlySource(t, s)
	assertRevokedW1Untouched(t, s)

	// 两份历史各自独立、互不覆盖，按申请顺序排列。
	ws := s.Waivers("s1")
	if len(ws) != 2 || ws[0].ID != "w1" || ws[1].ID != "w2" {
		t.Fatalf("免修历史应保留 w1、w2 两条且顺序不变，得到 %+v", ws)
	}
	if ws[1].Status != WaiverApproved || ws[1].Basis != reapplyNewBasis || ws[1].ReqID != "r1" {
		t.Fatalf("新申请应有自己的编号、依据与有效状态，得到 %+v", ws[1])
	}

	// 保存并重新加载：来源、学分与两份历史（含撤销原因）仍然完整一致。
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("重新加载应成功，existed=%v err=%v", existed, err)
	}
	assertW2IsOnlySource(t, loaded)
	assertRevokedW1Untouched(t, loaded)
	if w := loaded.Waiver("s1", "w2"); w == nil || w.Status != WaiverApproved ||
		w.Basis != reapplyNewBasis {
		t.Fatalf("重新加载后 w2 应仍为有效并保留新依据，得到 %+v", w)
	}
	if n := len(loaded.Waivers("s1")); n != 2 {
		t.Fatalf("重新加载后历史仍应为 2 条，得到 %d", n)
	}
}

// TestRevokedOldWaiverRetryDoesNotReviveBeforeAndAfterNew 旧编号按原要求、
// 原依据重试在新申请之前与之后都只返回已撤销的原申请：不新增历史、不授予
// 学分；新申请有效后的重试不能替换或撤销它。
func TestRevokedOldWaiverRetryDoesNotReviveBeforeAndAfterNew(t *testing.T) {
	t.Run("新申请之前", func(t *testing.T) {
		s := setupRevokedReapplyStore(t)

		// 重试只返回原已撤销申请，不新增历史、不复活、不标脏。
		for i := 0; i < 2; i++ {
			w, a, err := s.ApplyWaiver("s1", "r1", "w1", reapplyOldBasis)
			if err != nil || a != ActionExisted || w.Status != WaiverRevoked {
				t.Fatalf("第 %d 次重试应幂等返回已撤销原申请，得到 %+v action=%v err=%v",
					i+1, w, a, err)
			}
		}
		if n := len(s.Waivers("s1")); n != 1 {
			t.Fatalf("重试不应新增历史，应只有 1 条，得到 %d", n)
		}
		assertRevokedW1Untouched(t, s)
		if rep := s.CheckStudent("s1"); rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
			t.Fatalf("重试不能重新授予学分，得到学分=%d 未满足=%v",
				rep.TotalCredits, rep.Unmet)
		}

		// 旧申请不复活不影响随后用新编号申请。
		if w2, a, err := s.ApplyWaiver("s1", "r1", "w2", reapplyNewBasis); err != nil ||
			a != ActionCreated || w2.Status != WaiverApproved {
			t.Fatalf("重试后新编号申请仍应生效，得到 %+v action=%v err=%v", w2, a, err)
		}
		assertW2IsOnlySource(t, s)
		assertRevokedW1Untouched(t, s)
	})

	t.Run("新申请已经有效之后", func(t *testing.T) {
		s := setupRevokedReapplyStore(t)
		if w2, _, err := s.ApplyWaiver("s1", "r1", "w2", reapplyNewBasis); err != nil ||
			w2.Status != WaiverApproved {
			t.Fatalf("新编号申请应生效，得到 %+v err=%v", w2, err)
		}

		// 旧编号原样重试：返回已撤销的 w1，不能替换或撤销已经有效的 w2。
		w, a, err := s.ApplyWaiver("s1", "r1", "w1", reapplyOldBasis)
		if err != nil || a != ActionExisted || w.Status != WaiverRevoked {
			t.Fatalf("新申请有效后旧编号重试仍应返回已撤销原申请，得到 %+v action=%v err=%v",
				w, a, err)
		}
		if n := len(s.Waivers("s1")); n != 2 {
			t.Fatalf("重试不应新增历史，应只有 2 条，得到 %d", n)
		}
		assertRevokedW1Untouched(t, s)
		assertW2IsOnlySource(t, s)
	})

	t.Run("重新加载后的重试不标脏", func(t *testing.T) {
		// 保存后重新加载再重试已撤销申请：纯幂等返回，不应把记录标记为
		// 需要落盘（与命令行“重试不改文件”的行为一致）。
		s := setupRevokedReapplyStore(t)
		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if w, a, err := loaded.ApplyWaiver("s1", "r1", "w1", reapplyOldBasis); err != nil ||
			a != ActionExisted || w.Status != WaiverRevoked {
			t.Fatalf("重新加载后重试应返回已撤销原申请，得到 %+v action=%v err=%v", w, a, err)
		}
		if loaded.Dirty() {
			t.Fatal("幂等重试已撤销申请不应标记变更")
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
			t.Fatalf("重新加载后重试仍不能授予学分，得到学分=%d 未满足=%v",
				rep.TotalCredits, rep.Unmet)
		}
	})
}

// TestRevokedOldWaiverDifferentBasisConflictsBeforeAndAfterNew 旧编号携带不同
// 依据重试在新申请之前与之后都按内容冲突拒绝：原申请依据与状态不变，新申请
// 有效时核对仍以新申请为唯一来源，不能把换依据当作一次新申请。
func TestRevokedOldWaiverDifferentBasisConflictsBeforeAndAfterNew(t *testing.T) {
	t.Run("新申请之前换依据", func(t *testing.T) {
		s := setupRevokedReapplyStore(t)
		_, _, err := s.ApplyWaiver("s1", "r1", "w1", "换了一份依据")
		if err == nil || !strings.Contains(err.Error(), "w1") ||
			!strings.Contains(err.Error(), "提交内容不同") {
			t.Fatalf("旧编号换依据应按内容冲突拒绝并点名 w1，得到 %v", err)
		}
		assertRevokedW1Untouched(t, s)
		if n := len(s.Waivers("s1")); n != 1 {
			t.Fatalf("冲突拒绝不应新增历史，得到 %d 条", n)
		}
		if rep := s.CheckStudent("s1"); rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
			t.Fatalf("冲突后应仍为 0 学分、r1 未满足，得到学分=%d 未满足=%v",
				rep.TotalCredits, rep.Unmet)
		}
		// 冲突不挡住随后用新编号正常申请。
		if w2, _, err := s.ApplyWaiver("s1", "r1", "w2", reapplyNewBasis); err != nil ||
			w2.Status != WaiverApproved {
			t.Fatalf("冲突后新编号申请仍应生效，得到 %+v err=%v", w2, err)
		}
		assertW2IsOnlySource(t, s)
	})

	t.Run("新申请有效之后换依据", func(t *testing.T) {
		s := setupRevokedReapplyStore(t)
		if _, _, err := s.ApplyWaiver("s1", "r1", "w2", reapplyNewBasis); err != nil {
			t.Fatalf("新编号申请应生效：%v", err)
		}
		_, _, err := s.ApplyWaiver("s1", "r1", "w1", "另一份不同的依据")
		if err == nil || !strings.Contains(err.Error(), "w1") ||
			!strings.Contains(err.Error(), "提交内容不同") {
			t.Fatalf("新申请有效后旧编号换依据仍应按冲突拒绝，得到 %v", err)
		}
		assertRevokedW1Untouched(t, s)
		if n := len(s.Waivers("s1")); n != 2 {
			t.Fatalf("冲突拒绝不应新增历史，应保持 2 条，得到 %d", n)
		}
		assertW2IsOnlySource(t, s)
	})
}

// TestAnotherNewIDRejectedAfterNewWaiverValidAndPersisted w2 已经有效时再用
// 另一个新编号 w3 申请同一要求：拒绝入历史且原因点名现有有效免修 w2；
// w3 原样重试只返回已拒绝记录；原 w2 继续满足要求、总学分仍为 4；
// 保存重新加载后三份历史的状态、依据与原因完整保留。
func TestAnotherNewIDRejectedAfterNewWaiverValidAndPersisted(t *testing.T) {
	s := setupRevokedReapplyStore(t)
	if _, _, err := s.ApplyWaiver("s1", "r1", "w2", reapplyNewBasis); err != nil {
		t.Fatalf("w2 应生效：%v", err)
	}

	// 另一个新编号 w3：被拒绝但作为新记录保留在免修历史中，原因点名 w2。
	w3, a, err := s.ApplyWaiver("s1", "r1", "w3", reapplyThirdBasis)
	if err != nil || a != ActionCreated || w3.Status != WaiverRejected {
		t.Fatalf("已有有效免修时新编号应拒绝并新建拒绝记录，得到 %+v action=%v err=%v",
			w3, a, err)
	}
	if w3.ReqID != "r1" || w3.Basis != reapplyThirdBasis ||
		!strings.Contains(w3.Reason, "w2") {
		t.Fatalf("拒绝记录应保留原要求、依据并指出现有有效免修 w2，得到 %+v", w3)
	}
	if n := len(s.Waivers("s1")); n != 3 {
		t.Fatalf("拒绝记录应保留在历史中，应有 3 条，得到 %d", n)
	}

	// 核对：w2 仍是唯一来源、总学分 4；w3 列入被拒绝免修并附原因；
	// w1 仍为已撤销。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("原有效申请继续满足要求，应仍为 4 学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if st := rep.Requirements[0]; !st.Satisfied || st.Source != "waiver" ||
		st.WaiverID != "w2" {
		t.Fatalf("来源应继续指向 w2，得到 %+v", st)
	}
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("应列出 1 条被拒绝免修 w3，得到 %+v", rep.RejectedWaivers)
	}
	rj := rep.RejectedWaivers[0]
	if rj.Waiver.ID != "w3" || rj.Waiver.Basis != reapplyThirdBasis ||
		!strings.Contains(rj.Reason, "w2") {
		t.Fatalf("被拒绝记录应保留 w3、依据与点名 w2 的原因，得到 %+v", rj)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w1" {
		t.Fatalf("w1 应仍列为已撤销，得到 %v", rep.RevokedWaivers)
	}

	// w3 原样重试：返回原拒绝记录，不新增历史、不改变学分与来源。
	if w, a, err := s.ApplyWaiver("s1", "r1", "w3", reapplyThirdBasis); err != nil ||
		a != ActionExisted || w.Status != WaiverRejected {
		t.Fatalf("w3 原样重试应幂等返回已拒绝记录，得到 %+v action=%v err=%v", w, a, err)
	}
	if n := len(s.Waivers("s1")); n != 3 {
		t.Fatalf("w3 重试不应新增历史，应仍为 3 条，得到 %d", n)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].WaiverID != "w2" || len(rep.RejectedWaivers) != 1 {
		t.Fatalf("w3 重试后学分、来源与拒绝历史都应不变，得到 %+v", rep)
	}

	// 保存并重新加载：三份历史的编号、依据、状态、原因与核对结论一致。
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("含撤销/有效/拒绝三份历史的记录应正常加载：%v", err)
	}
	assertW2IsOnlySource(t, loaded)
	assertRevokedW1Untouched(t, loaded)
	if w := loaded.Waiver("s1", "w3"); w == nil || w.Status != WaiverRejected ||
		w.Basis != reapplyThirdBasis || !strings.Contains(w.Reason, "w2") {
		t.Fatalf("重新加载后 w3 应仍为已拒绝并保留原因，得到 %+v", w)
	}
	if n := len(loaded.Waivers("s1")); n != 3 {
		t.Fatalf("重新加载后历史应仍为 3 条，得到 %d", n)
	}
}
