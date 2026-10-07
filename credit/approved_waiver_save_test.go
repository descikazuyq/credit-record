package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“保存时同一学生名下的同一项要求不得同时有两份有效免修”。正常
// 申请流程（ApplyWaiver）与读取记录文件（Load）都拒绝这类冲突，但调用方
// 取得免修记录后可以直接修改其内容——把另一份申请改成有效，或把一份有效
// 申请的目标改到已由免修满足的要求——因此用例先经公开接口建立结构完整的
// 记录，再直接改免修状态或目标要求，聚焦 Save 这一路径：
//   - 同一学生、同一要求下两份不同编号的有效免修（依据相同也算两份；该要求
//     另有通过修读也不能掩盖；冲突申请之间夹着其他合法历史结果一样），整次
//     保存必须拒绝：错误说明是有效免修冲突，并点名目标文件、所属学生、要求
//     编号与冲突的两份免修编号；
//   - 目标文件原本不存在时不创建记录文件，也不残留临时文件；已有文件的全部
//     字节原样保留、仍可正常读取与核对；
//   - 待保存的内存记录保持提交时的内容：不删申请、不替调用方选定有效免修、
//     不改状态/目标要求/依据；调用方消除冲突后可继续用原有保存功能；
//   - 冲突只按（所属学生、目标要求）完整编号判定：编号前后有空白的是不同
//     要求；不同学生各自拥有同号要求、同号免修合法；同一学生的不同要求各有
//     一份有效免修合法；一份有效免修与已拒绝、已撤销历史共存合法。

// saveWaiverStore 建立两名学生、两门课程与各自要求，供用例登记免修后再改出
// 有效免修冲突：s1 名下有要求 r1（课程 c1）与 r2（课程 c2），s2 名下有同号
// 要求 r1（课程 c2）。
func saveWaiverStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustCourse(t, s, "c2", "线性代数", 3)
	mustReq(t, s, "s1", "r1", "c1")
	mustReq(t, s, "s1", "r2", "c2")
	mustReq(t, s, "s2", "r1", "c2")
	return s
}

// mustWaiver 经公开接口提交免修申请并断言得到的状态。
func mustWaiver(t *testing.T, s *Store, student, req, id, basis string, want WaiverStatus) *Waiver {
	t.Helper()
	w, a, err := s.ApplyWaiver(student, req, id, basis)
	if err != nil || a != ActionCreated {
		t.Fatalf("ApplyWaiver(%q,%q,%q) = action %v, err %v", student, req, id, a, err)
	}
	if w.Status != want {
		t.Fatalf("免修 %s 状态应为 %s，得到 %s", id, want, w.Status)
	}
	return w
}

// assertSaveWaiverConflictError 断言保存错误说明是有效免修冲突，并点名目标
// 文件、所属学生、要求编号与冲突的两份免修编号，返回解析后的类型化错误。
func assertSaveWaiverConflictError(t *testing.T, err error, path,
	student, req, first, second string) *duplicateApprovedWaiverError {
	t.Helper()
	if err == nil {
		t.Fatalf("学生 %s 的要求 %s 有两份有效免修 %s 与 %s 时必须拒绝保存",
			student, req, first, second)
	}
	var dwe *duplicateApprovedWaiverError
	if !errors.As(err, &dwe) {
		t.Fatalf("应返回 *duplicateApprovedWaiverError，得到 %T：%v", err, err)
	}
	if dwe.student != student || dwe.req != req ||
		dwe.first != first || dwe.second != second {
		t.Fatalf("类型化错误定位不符：应为 (%s,%s) 的 %s 与 %s，得到 %+v",
			student, req, first, second, dwe)
	}
	msg := err.Error()
	for _, want := range []string{path, "有效免修", student, req, first, second} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误应说明有效免修冲突并点名目标文件、学生、要求与双方（缺 %q），得到：%v",
				want, err)
		}
	}
	return dwe
}

// assertNoFileLeftBehind 断言目标文件未创建且目录下没有残留临时文件。
func assertNoFileLeftBehind(t *testing.T, path string) {
	t.Helper()
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("拒绝保存后不得创建记录文件，stat err=%v", statErr)
	}
	leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
	}
}

// TestSaveRejectsDuplicateApprovedWaiver 覆盖各类“同一要求两份有效免修”的
// 组合：改状态生效、改目标要求、依据相同、另有通过修读、夹着合法历史，结论
// 都是整次拒绝保存。
func TestSaveRejectsDuplicateApprovedWaiver(t *testing.T) {
	t.Run("把另一份申请改成有效", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 第二份申请因已有有效免修被正常拒绝，随后调用方直接把它改成有效。
		w2 := mustWaiver(t, s, "s1", "r1", "w2", "课程已修过", WaiverRejected)
		w2.Status = WaiverApproved

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
		assertNoFileLeftBehind(t, path)
	})

	t.Run("把有效申请的目标改到已由免修满足的要求", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		w2 := mustWaiver(t, s, "s1", "r2", "w2", "课程已修过", WaiverApproved)
		// 调用方把 w2 的目标从 r2 改到已有有效免修的 r1。
		w2.ReqID = "r1"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
		assertNoFileLeftBehind(t, path)
	})

	t.Run("两份申请依据相同仍算两份", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		w2 := mustWaiver(t, s, "s1", "r1", "w2", "竞赛获奖", WaiverRejected)
		w2.Status = WaiverApproved

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
	})

	t.Run("该要求另有通过修读也不能掩盖冲突", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		w2 := mustWaiver(t, s, "s1", "r1", "w2", "课程已修过", WaiverRejected)
		w2.Status = WaiverApproved

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
	})

	t.Run("冲突申请之间夹着其他合法历史", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 中间隔着：r1 的又一份被拒绝申请、r2 的有效免修、其他学生的有效免修。
		mustWaiver(t, s, "s1", "r1", "w9", "重复申请", WaiverRejected)
		mustWaiver(t, s, "s1", "r2", "w3", "课程已修过", WaiverApproved)
		mustWaiver(t, s, "s2", "r1", "w1", "竞赛获奖", WaiverApproved)
		w2 := mustWaiver(t, s, "s1", "r1", "w2", "另一依据", WaiverRejected)
		w2.Status = WaiverApproved

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		// 报错仍按记录先后点名最先相撞的一对：s1/r1 的 w1 与 w2。
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
		assertNoFileLeftBehind(t, path)
	})
}

// TestSaveWaiverConflictKeepsExistingFileUntouched 目标文件已存在且内容完好
// 时，含有效免修冲突的保存必须失败：原文件逐字节保留、仍可读取核对；冲突的
// 内存记录保持提交时的内容；调用方自行消除冲突后可继续正常保存。
func TestSaveWaiverConflictKeepsExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	// 先落一份完好文件：s1 的 r1 由有效免修 w1 满足（4 学分），r2 未满足。
	good := saveWaiverStore(t)
	mustWaiver(t, good, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
	if err := good.Save(path); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// 程序重新取得这份记录，再提交 w2（被正常拒绝）并直接把它改成有效。
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("前置文件应正常读取：existed=%v err=%v", existed, err)
	}
	w2 := mustWaiver(t, loaded, "s1", "r1", "w2", "课程已修过", WaiverRejected)
	w2.Status = WaiverApproved
	saveErr := loaded.Save(path)
	assertSaveWaiverConflictError(t, saveErr, path, "s1", "r1", "w1", "w2")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("原文件应继续可读：%v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("拒绝保存后已有文件必须逐字节保持原样\nwant=%q\n got=%q", raw, got)
	}
	leftover, _ := filepath.Glob(filepath.Join(dir, ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
	}

	// 原文件仍可正常读取与核对：s1 凭 w1 得 4 学分，文件中没有 w2。
	reloaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("原文件应仍可正常读取：existed=%v err=%v", existed, err)
	}
	rep := reloaded.CheckStudent("s1")
	if rep.TotalCredits != 4 || rep.Requirements[0].Source != "waiver" ||
		rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("原文件核对结果应保持不变，得到 %+v", rep)
	}
	if w := reloaded.Waiver("s1", "w2"); w != nil {
		t.Fatalf("坏记录不得落盘，原文件中不应出现 w2，得到 %+v", w)
	}

	// 内存中的冲突记录保持提交时的内容：两份申请都在，状态、目标、依据不改。
	w1 := loaded.Waiver("s1", "w1")
	if w1 == nil || w1.Status != WaiverApproved || w1.ReqID != "r1" || w1.Basis != "竞赛获奖" {
		t.Fatalf("w1 不得被改动，得到 %+v", w1)
	}
	if w2 := loaded.Waiver("s1", "w2"); w2 == nil || w2.Status != WaiverApproved ||
		w2.ReqID != "r1" || w2.Basis != "课程已修过" {
		t.Fatalf("w2 应保持调用方改成的样子，保存功能不得代为选定或改回，得到 %+v", w2)
	}
	if n := len(loaded.Waivers("s1")); n != 2 {
		t.Fatalf("不得删除任何免修申请，得到 %d 条", n)
	}

	// 由调用方自行消除冲突（撤销 w2）后，同一路径即可正常保存。
	w2.Status = WaiverRevoked
	if err := loaded.Save(path); err != nil {
		t.Fatalf("调用方消除冲突后应能正常保存：%v", err)
	}
	final, _, err := Load(path)
	if err != nil {
		t.Fatalf("消除冲突后的文件应可正常读取：%v", err)
	}
	if w := final.Waiver("s1", "w2"); w == nil || w.Status != WaiverRevoked {
		t.Fatalf("消除冲突后的修改应原样落盘，得到 %+v", w)
	}
}

// TestSaveWaiverConflictDoesNotMutateMemory 拒绝保存不得改动待保存内存记录的
// 任何内容：两份冲突申请、其他有效免修、被拒绝/已撤销历史都保持提交时的样子。
func TestSaveWaiverConflictDoesNotMutateMemory(t *testing.T) {
	s := saveWaiverStore(t)
	mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
	mustWaiver(t, s, "s1", "r2", "w3", "课程已修过", WaiverApproved)
	w2 := mustWaiver(t, s, "s1", "r1", "w2", "另一依据", WaiverRejected)
	w2.Status = WaiverApproved
	// 其他学生的合法历史与一条已撤销历史也不受影响。
	mustWaiver(t, s, "s2", "r1", "w1", "竞赛获奖", WaiverApproved)
	if _, _, err := s.RevokeWaiver("s2", "w1", "材料作废"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err == nil {
		t.Fatal("同一要求有两份有效免修时必须拒绝保存")
	}

	for id, want := range map[string]WaiverStatus{
		"w1": WaiverApproved, "w2": WaiverApproved, "w3": WaiverApproved,
	} {
		w := s.Waiver("s1", id)
		if w == nil || w.Status != want {
			t.Fatalf("s1 的免修 %s 应保持状态 %s，得到 %+v", id, want, w)
		}
	}
	if w := s.Waiver("s1", "w2"); w.ReqID != "r1" || w.Basis != "另一依据" {
		t.Fatalf("w2 的目标要求与依据不得被改动，得到 %+v", w)
	}
	if w := s.Waiver("s2", "w1"); w == nil || w.Status != WaiverRevoked || w.Reason != "材料作废" {
		t.Fatalf("s2 的已撤销历史不得被改动，得到 %+v", w)
	}
	if n := len(s.waivers); n != 4 {
		t.Fatalf("不得删除任何免修记录，得到 %d 条", n)
	}
}

// TestSaveLegalWaiverArrangementsAccepted 合法的免修安排继续正常保存，学分与
// 来源规则保持不变：
//   - 不同学生各自拥有同号要求、同号免修；
//   - 同一学生的不同要求各有一份有效免修；
//   - 编号前后有空白的是不同要求，各自的有效免修互不冲突；
//   - 一份有效免修与已拒绝、已撤销历史共存，失效申请不占有效名额；
//   - 同一要求只计一份学分，有效免修说明当前来源，修读历史继续保留。
func TestSaveLegalWaiverArrangementsAccepted(t *testing.T) {
	t.Run("不同学生同号要求同号免修", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		mustWaiver(t, s, "s2", "r1", "w1", "竞赛获奖", WaiverApproved)

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("不同学生各自的同号有效免修应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 4 ||
			rep.Requirements[0].WaiverID != "w1" {
			t.Fatalf("s1 应凭本人 w1 得 4 学分，得到 %+v", rep)
		}
		if rep := loaded.CheckStudent("s2"); rep.TotalCredits != 3 ||
			rep.Requirements[0].WaiverID != "w1" {
			t.Fatalf("s2 应凭本人 w1 得 3 学分，得到 %+v", rep)
		}
		if loaded.Waiver("s1", "w1") == loaded.Waiver("s2", "w1") {
			t.Fatal("两名学生的同号免修应仍是两条独立记录")
		}
	})

	t.Run("同一学生不同要求各有一份有效免修", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		mustWaiver(t, s, "s1", "r2", "w2", "课程已修过", WaiverApproved)

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("不同要求各有一份有效免修应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 7 || len(rep.Unmet) != 0 {
			t.Fatalf("两项要求应各计一份学分共 7 学分，得到 %+v", rep)
		}
	})

	t.Run("编号前后有空白的是不同要求", func(t *testing.T) {
		s := NewStore()
		mustStudent(t, s, "s1")
		mustCourse(t, s, "c1", "高等数学", 4)
		mustCourse(t, s, "c2", "线性代数", 3)
		mustReq(t, s, "s1", "r1", "c1")
		mustReq(t, s, "s1", " r1 ", "c2")
		// “r1”与“ r1 ”是两项不同要求，各自的有效免修互不冲突。
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		mustWaiver(t, s, "s1", " r1 ", "w2", "课程已修过", WaiverApproved)

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("带空白编号的不同要求各有有效免修应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 7 {
			t.Fatalf("两项不同要求应各计一份学分共 7 学分，得到 %+v", rep)
		}
		if loaded.Waiver("s1", "w1").ReqID != "r1" || loaded.Waiver("s1", "w2").ReqID != " r1 " {
			t.Fatal("编号原文不得因保存被修剪或合并")
		}
	})

	t.Run("有效免修与已拒绝已撤销历史共存", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 同一要求下：一份被正常拒绝的申请与一份已撤销的旧申请，都不占有效名额。
		mustWaiver(t, s, "s1", "r1", "w2", "重复申请", WaiverRejected)
		w0 := mustWaiver(t, s, "s1", "r2", "w0", "旧依据", WaiverApproved)
		if _, _, err := s.RevokeWaiver("s1", "w0", "材料作废"); err != nil {
			t.Fatal(err)
		}
		w0.ReqID = "r1" // 调用方把已撤销申请的目标改到 r1：失效历史不占有效名额。

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("失效申请不占有效名额，应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		// 所有历史按原内容保留；r1 仍由唯一的有效免修 w1 满足，只计一份学分。
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 4 || rep.Requirements[0].WaiverID != "w1" {
			t.Fatalf("r1 应凭 w1 计 4 学分，得到 %+v", rep)
		}
		if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver.ID != "w2" {
			t.Fatalf("被拒绝历史应原样保留，得到 %+v", rep.RejectedWaivers)
		}
		if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w0" {
			t.Fatalf("已撤销历史应原样保留，得到 %v", rep.RevokedWaivers)
		}
	})

	t.Run("同一要求只计一份学分_免修说明来源_修读历史保留", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("通过修读与一份有效免修并存应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 4 {
			t.Fatalf("同一要求只计一份 4 学分，得到 %d", rep.TotalCredits)
		}
		st := rep.Requirements[0]
		if st.Source != "waiver" || st.WaiverID != "w1" ||
			len(st.PassedEnrollmentIDs) != 1 || st.PassedEnrollmentIDs[0] != "e1" {
			t.Fatalf("有效免修应说明来源且修读历史保留，得到 %+v", st)
		}
	})
}
