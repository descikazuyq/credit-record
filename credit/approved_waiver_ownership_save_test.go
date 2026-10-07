package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“保存时每份有效免修的归属必须成立”：所属学生必须已登记，目标
// 要求必须实际存在于该学生本人名下。正常申请流程（ApplyWaiver）只会为本人
// 名下确实存在的要求生效免修，读取记录文件（Load/loadData）也拒绝归属不
// 成立的有效免修；但调用方取得免修记录后可以直接修改其目标要求或所属学生，
// 即使改成了不存在的要求、只属于另一名学生的要求，或未登记的学生，保存也
// 曾报告成功——随后再读取这份文件却被判为内容损坏。因此用例先经公开接口
// 建立结构完整的记录，再直接改免修的目标要求、所属学生或状态，聚焦 Save
// 这一路径：
//   - 目标要求改成不存在的编号、改成只属于另一名学生的同号要求、所属学生
//     改成未登记编号、曾被拒绝的申请改成有效但没补齐本人要求，整次保存都
//     必须拒绝：错误点名目标文件、免修编号、所属学生编号与目标要求编号，
//     并说明学生不存在或本人名下不存在该要求；
//   - 目标文件原本不存在时不创建记录文件，也不残留临时文件；已有文件的全部
//     字节原样保留、仍可正常读取与核对；
//   - 待保存的内存记录保持提交时的内容：不跳过这份申请、不自动创建要求、
//     不转移学生归属、不改成已拒绝状态、不删除申请；调用方补齐归属后可
//     继续用原有保存功能；
//   - 判定只反映本次待保存记录的内容，编号按完整文字匹配；依据充分、同一
//     要求没有第二份有效免修都不能替代归属条件；已拒绝的申请允许保留当时
//     不存在的要求，已撤销申请沿用现有规则。

// assertSaveWaiverOwnershipError 断言保存错误点名目标文件、免修编号、所属
// 学生编号与目标要求编号，并按 noStudent 说明是学生不存在还是本人名下不
// 存在该要求，返回解析后的类型化错误。
func assertSaveWaiverOwnershipError(t *testing.T, err error, path,
	waiver, student, req string, noStudent bool) *invalidApprovedWaiverError {
	t.Helper()
	if err == nil {
		t.Fatalf("有效免修 %s（学生 %s、要求 %s）归属不成立时必须拒绝保存",
			waiver, student, req)
	}
	var owe *invalidApprovedWaiverError
	if !errors.As(err, &owe) {
		t.Fatalf("应返回 *invalidApprovedWaiverError，得到 %T：%v", err, err)
	}
	if owe.waiver != waiver || owe.student != student || owe.req != req ||
		owe.noStudent != noStudent {
		t.Fatalf("类型化错误定位不符：应为免修 %s（学生 %s、要求 %s、noStudent=%v），得到 %+v",
			waiver, student, req, noStudent, owe)
	}
	msg := err.Error()
	for _, want := range []string{path, waiver, student, req} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误应点名目标文件、免修编号、学生编号与要求编号（缺 %q），得到：%v",
				want, err)
		}
	}
	if noStudent {
		if !strings.Contains(msg, "未登记") {
			t.Fatalf("学生不存在时错误应说明学生未登记，得到：%v", err)
		}
	} else if !strings.Contains(msg, "名下") {
		t.Fatalf("要求不在本人名下时错误应说明这一点，得到：%v", err)
	}
	return owe
}

// TestSaveRejectsApprovedWaiverWithInvalidOwnership 覆盖各类“有效免修归属不
// 成立”的组合：目标要求不存在、要求只属于另一名学生、所属学生未登记、曾被
// 拒绝的申请改成有效但没补齐本人要求，结论都是整次拒绝保存。
func TestSaveRejectsApprovedWaiverWithInvalidOwnership(t *testing.T) {
	t.Run("目标要求改成不存在的编号", func(t *testing.T) {
		s := saveWaiverStore(t)
		w := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 调用方把目标要求改成任何学生名下都不存在的编号。
		w.ReqID = "r9"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "w1", "s1", "r9", false)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("目标要求只属于另一名学生", func(t *testing.T) {
		s := saveWaiverStore(t)
		// s1 名下没有 r3，s2 名下有；同号要求在他人名下不能作为放行依据。
		mustReq(t, s, "s2", "r3", "c1")
		w := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		w.ReqID = "r3"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "w1", "s1", "r3", false)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("所属学生改成未登记编号", func(t *testing.T) {
		s := saveWaiverStore(t)
		w := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 调用方把所属学生改成未登记的编号；r1 在 s1 名下存在也不能放行。
		w.StudentID = "s9"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "w1", "s9", "r1", true)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("曾被拒绝的申请改成有效但没补齐本人要求", func(t *testing.T) {
		s := saveWaiverStore(t)
		// 目标要求不存在，申请被正常拒绝并保留在历史中；调用方随后直接把它
		// 改成有效，但没有为本人补齐要求。
		w := mustWaiver(t, s, "s1", "r9", "w1", "竞赛获奖", WaiverRejected)
		w.Status = WaiverApproved

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "w1", "s1", "r9", false)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("依据充分且无第二份有效免修也不能替代归属条件", func(t *testing.T) {
		s := saveWaiverStore(t)
		w := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 依据非空、该目标也只有这一份有效免修，但 r5 只在 s2 名下存在。
		mustReq(t, s, "s2", "r5", "c1")
		w.ReqID = "r5"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "w1", "s1", "r5", false)
		assertNoFileLeftBehind(t, path)
	})
}

// TestSaveWaiverOwnershipKeepsExistingFileUntouched 目标文件已存在且内容完好
// 时，含归属不成立有效免修的保存必须失败：原文件逐字节保留、仍可读取核对；
// 内存记录保持提交时的内容；调用方补齐归属后可继续正常保存。
func TestSaveWaiverOwnershipKeepsExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	// 先落一份完好文件：s1 的 r1 由有效免修 w1 满足（4 学分）。
	good := saveWaiverStore(t)
	mustWaiver(t, good, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
	if err := good.Save(path); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// 程序重新取得这份记录，再把 w1 的目标改到不存在的 r9。
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("前置文件应正常读取：existed=%v err=%v", existed, err)
	}
	w1 := loaded.Waiver("s1", "w1")
	if w1 == nil {
		t.Fatal("前置文件中应存在免修 w1")
	}
	w1.ReqID = "r9"
	saveErr := loaded.Save(path)
	assertSaveWaiverOwnershipError(t, saveErr, path, "w1", "s1", "r9", false)

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

	// 原文件仍可正常读取与核对：s1 凭 w1 得 4 学分。
	reloaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("原文件应仍可正常读取：existed=%v err=%v", existed, err)
	}
	if rep := reloaded.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("原文件核对结果应保持不变，得到 %+v", rep)
	}

	// 内存中的记录保持提交时的内容：w1 仍在、仍是有效、仍指向 r9，未被删除、
	// 未被改成已拒绝、未被转移归属，也没有为 r9 自动创建要求。
	if w := loaded.Waiver("s1", "w1"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r9" || w.Basis != "竞赛获奖" {
		t.Fatalf("w1 应保持调用方改成的样子，得到 %+v", w)
	}
	if r := loaded.Requirement("s1", "r9"); r != nil {
		t.Fatalf("不得自动创建要求 r9，得到 %+v", r)
	}
	if n := len(loaded.Waivers("s1")); n != 1 {
		t.Fatalf("不得删除或新增免修申请，得到 %d 条", n)
	}

	// 由调用方自行补齐归属（把目标改回本人名下的 r1）后即可正常保存。
	w1.ReqID = "r1"
	if err := loaded.Save(path); err != nil {
		t.Fatalf("归属修正后应能正常保存：%v", err)
	}
	final, _, err := Load(path)
	if err != nil {
		t.Fatalf("修正归属后的文件应可正常读取：%v", err)
	}
	if rep := final.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("修正归属后 w1 应继续满足 r1 计 4 学分，得到 %+v", rep)
	}
}

// TestSaveWaiverOwnershipDoesNotMutateMemory 拒绝保存不得改动待保存内存记录的
// 任何内容：归属不成立的有效免修、其他学生的合法免修、被拒绝/已撤销历史都
// 保持提交时的样子。
func TestSaveWaiverOwnershipDoesNotMutateMemory(t *testing.T) {
	s := saveWaiverStore(t)
	w1 := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
	mustWaiver(t, s, "s2", "r1", "w1", "竞赛获奖", WaiverApproved)
	// 同一学生同一要求下一份被正常拒绝的申请（目标当时不存在）。
	mustWaiver(t, s, "s1", "r9", "w2", "另一依据", WaiverRejected)
	w1.ReqID = "r9" // 调用方把有效免修的目标改到不存在的 r9。

	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err == nil {
		t.Fatal("有效免修归属不成立时必须拒绝保存")
	}

	if w := s.Waiver("s1", "w1"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r9" || w.Basis != "竞赛获奖" {
		t.Fatalf("w1 不得被改动，得到 %+v", w)
	}
	if w := s.Waiver("s2", "w1"); w == nil || w.Status != WaiverApproved || w.ReqID != "r1" {
		t.Fatalf("其他学生的合法免修不得被改动，得到 %+v", w)
	}
	if w := s.Waiver("s1", "w2"); w == nil || w.Status != WaiverRejected || w.ReqID != "r9" {
		t.Fatalf("被拒绝历史不得被改动，得到 %+v", w)
	}
	if n := len(s.waivers); n != 3 {
		t.Fatalf("不得删除任何免修记录，得到 %d 条", n)
	}
	if r := s.Requirement("s1", "r9"); r != nil {
		t.Fatalf("不得自动创建要求 r9，得到 %+v", r)
	}
}

// TestSaveWaiverOwnershipLegalArrangements 归属合法的记录继续正常保存：
//   - 两名学生各自拥有同号要求与同号有效免修，是独立的合法记录；
//   - 编号前后有空白的是不同学生/不同要求，各自的有效免修按完整编号归属；
//   - 已拒绝的申请允许保留当时不存在的目标要求及原拒绝原因，已撤销申请
//     沿用现有规则，都不因本次检查失去历史记录。
func TestSaveWaiverOwnershipLegalArrangements(t *testing.T) {
	t.Run("两名学生同号要求同号免修各自独立", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		mustWaiver(t, s, "s2", "r1", "w1", "竞赛获奖", WaiverApproved)

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("两名学生各自的同号有效免修应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 4 {
			t.Fatalf("s1 应凭本人 w1 得 4 学分，得到 %+v", rep)
		}
		if rep := loaded.CheckStudent("s2"); rep.TotalCredits != 3 {
			t.Fatalf("s2 应凭本人 w1 得 3 学分，得到 %+v", rep)
		}
	})

	t.Run("带空白编号按完整文字归属", func(t *testing.T) {
		s := NewStore()
		mustStudent(t, s, "s1")
		mustStudent(t, s, " s1 ")
		mustCourse(t, s, "c1", "高等数学", 4)
		mustCourse(t, s, "c2", "线性代数", 3)
		mustReq(t, s, "s1", "r1", "c1")
		mustReq(t, s, " s1 ", "r1", "c2")
		// “s1”与“ s1 ”是两名不同学生，各自名下的 r1 是两项不同要求。
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		mustWaiver(t, s, " s1 ", "r1", "w1", "课程已修过", WaiverApproved)

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("带空白编号学生名下的有效免修应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 4 {
			t.Fatalf("s1 应凭本人 w1 得 4 学分，得到 %+v", rep)
		}
		if rep := loaded.CheckStudent(" s1 "); rep.TotalCredits != 3 {
			t.Fatalf("“ s1 ”应凭本人 w1 得 3 学分，得到 %+v", rep)
		}
	})

	t.Run("已拒绝申请保留当时不存在的要求", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 目标当时不存在而被拒绝的申请：保留原要求编号与拒绝原因，不因本次
		// 检查失去历史记录。
		mustWaiver(t, s, "s1", "r9", "w2", "另一依据", WaiverRejected)

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("已拒绝申请保留当时不存在的要求应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 4 || rep.Requirements[0].WaiverID != "w1" {
			t.Fatalf("r1 应凭 w1 计 4 学分，得到 %+v", rep)
		}
		if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver.ID != "w2" ||
			rep.RejectedWaivers[0].Waiver.ReqID != "r9" {
			t.Fatalf("被拒绝历史应原样保留，得到 %+v", rep.RejectedWaivers)
		}
	})
}
