package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“保存时每份有效免修都必须归属到本人名下的真实要求”。正常申请
// 流程（ApplyWaiver）只会为本人名下真实存在的要求生效免修，读取记录文件
// （Load）也拒绝指向不存在要求的有效免修；但调用方取得免修记录后可以直接
// 修改其内容——改目标要求编号、改所属学生，或把曾因目标不存在而被拒绝的
// 申请直接改成有效——因此用例先经公开接口建立结构完整的记录，再直接改免修
// 内容，聚焦 Save 这一路径：
//   - 目标要求不存在、目标要求只属于另一名学生、所属学生未登记、被拒绝申请
//     改成有效却没补齐本人要求，整次保存都必须拒绝：错误点名目标文件、免修
//     编号、所属学生编号与目标要求编号，说明学生不存在或本人名下不存在该
//     要求；
//   - 目标文件原本不存在时不创建记录文件，也不残留临时文件；已有文件的全部
//     字节原样保留、仍可正常读取与核对；
//   - 待保存的内存记录保持提交时的内容：不跳过该申请、不自动创建要求、不
//     转移学生归属、不改状态、不删除申请；调用方修正归属后可继续正常保存；
//   - 编号按完整文字匹配：大小写与前后空白都是编号内容；已拒绝申请保留当时
//     不存在的目标要求与原拒绝原因照常保存，已撤销申请沿用现有规则。

// assertSaveWaiverOwnershipError 断言保存错误点名目标文件、免修编号、所属
// 学生编号与目标要求编号，并按 studentMissing 说明是学生不存在还是本人名下
// 不存在该要求，返回解析后的类型化错误。
func assertSaveWaiverOwnershipError(t *testing.T, err error, path,
	student, waiver, req string, studentMissing bool) *approvedWaiverOwnershipError {
	t.Helper()
	if err == nil {
		t.Fatalf("有效免修 %s（学生 %s、要求 %s）归属无效时必须拒绝保存",
			waiver, student, req)
	}
	var owe *approvedWaiverOwnershipError
	if !errors.As(err, &owe) {
		t.Fatalf("应返回 *approvedWaiverOwnershipError，得到 %T：%v", err, err)
	}
	if owe.student != student || owe.waiver != waiver || owe.req != req ||
		owe.studentMissing != studentMissing {
		t.Fatalf("类型化错误定位不符：应为 (%s,%s,%s) studentMissing=%v，得到 %+v",
			student, waiver, req, studentMissing, owe)
	}
	msg := err.Error()
	for _, want := range []string{path, "有效免修", student, waiver, req} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误应点名目标文件、免修编号、所属学生与目标要求（缺 %q），得到：%v",
				want, err)
		}
	}
	if studentMissing {
		if !strings.Contains(msg, "不存在") {
			t.Fatalf("错误应说明学生不存在，得到：%v", err)
		}
	} else if !strings.Contains(msg, "名下") {
		t.Fatalf("错误应说明本人名下不存在该要求，得到：%v", err)
	}
	return owe
}

// TestSaveRejectsApprovedWaiverWithDanglingTarget 覆盖各类“有效免修无法归属
// 到本人名下真实要求”的组合：改出不存在的目标要求、改指只属于另一名学生的
// 要求、改出未登记的所属学生、把曾因目标不存在而被拒绝的申请直接改成有效、
// 编号空白与大小写差异，结论都是整次拒绝保存。
func TestSaveRejectsApprovedWaiverWithDanglingTarget(t *testing.T) {
	t.Run("把有效免修的目标改成不存在的要求", func(t *testing.T) {
		s := saveWaiverStore(t)
		w1 := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 调用方把目标从真实存在的 r1 改成不存在的 r9。
		w1.ReqID = "r9"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "s1", "w1", "r9", false)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("目标要求只在另一名学生名下存在不能放行", func(t *testing.T) {
		s := saveWaiverStore(t)
		// r3 只登记在 s2 名下；s1 的有效免修 w1 被改指 r3。
		mustReq(t, s, "s2", "r3", "c1")
		w1 := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		w1.ReqID = "r3"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		// r3 在 s2 名下确实存在，但 w1 属于 s1：本人名下没有该要求，必须拒绝。
		assertSaveWaiverOwnershipError(t, err, path, "s1", "w1", "r3", false)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("把所属学生改成未登记编号", func(t *testing.T) {
		s := saveWaiverStore(t)
		w1 := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 调用方把所属学生改成未登记的 s9：s1 名下确有 r1 也不能放行。
		w1.StudentID = "s9"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "s9", "w1", "r1", true)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("曾因目标不存在被拒绝的申请改成有效", func(t *testing.T) {
		s := saveWaiverStore(t)
		// w2 因目标 r9 不存在被正常拒绝并记入历史；调用方直接把它改成有效，
		// 却没有为 s1 补齐 r9 这项要求。
		w2 := mustWaiver(t, s, "s1", "r9", "w2", "课程已修过", WaiverRejected)
		w2.Status = WaiverApproved

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "s1", "w2", "r9", false)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("编号前后空白是编号内容", func(t *testing.T) {
		s := saveWaiverStore(t)
		w1 := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 只有“r1”存在，“ r1 ”是另一项并不存在的要求，不能修剪后匹配。
		w1.ReqID = " r1 "

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "s1", "w1", " r1 ", false)
		assertNoFileLeftBehind(t, path)
	})

	t.Run("大小写不同是不同要求", func(t *testing.T) {
		s := saveWaiverStore(t)
		w1 := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// 只有“r1”存在，“R1”不是同一项要求。
		w1.ReqID = "R1"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverOwnershipError(t, err, path, "s1", "w1", "R1", false)
		assertNoFileLeftBehind(t, path)
	})
}

// TestSaveWaiverOwnershipKeepsExistingFileUntouched 目标文件已存在且内容完好
// 时，含归属问题的有效免修的保存必须失败：原文件逐字节保留、仍可读取核对；
// 内存记录保持提交时的内容；调用方自行修正归属后可继续正常保存。
func TestSaveWaiverOwnershipKeepsExistingFileUntouched(t *testing.T) {
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

	// 程序重新取得这份记录，再把 w1 的目标改成不存在的要求 r9。
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("前置文件应正常读取：existed=%v err=%v", existed, err)
	}
	w1 := loaded.Waiver("s1", "w1")
	if w1 == nil || w1.Status != WaiverApproved {
		t.Fatalf("前置文件中 w1 应为有效免修，得到 %+v", w1)
	}
	w1.ReqID = "r9"
	saveErr := loaded.Save(path)
	assertSaveWaiverOwnershipError(t, saveErr, path, "s1", "w1", "r9", false)

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

	// 原文件仍可正常读取与核对：s1 凭 w1 得 4 学分，目标仍是 r1。
	reloaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("原文件应仍可正常读取：existed=%v err=%v", existed, err)
	}
	rep := reloaded.CheckStudent("s1")
	if rep.TotalCredits != 4 || rep.Requirements[0].Source != "waiver" ||
		rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("原文件核对结果应保持不变，得到 %+v", rep)
	}
	if w := reloaded.Waiver("s1", "w1"); w == nil || w.ReqID != "r1" {
		t.Fatalf("坏记录不得落盘，原文件中 w1 的目标应仍是 r1，得到 %+v", w)
	}

	// 内存中的记录保持提交时的内容：w1 仍是调用方改成的样子，不被代为改回。
	if w := loaded.Waiver("s1", "w1"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r9" || w.Basis != "竞赛获奖" {
		t.Fatalf("w1 应保持调用方改成的样子，保存功能不得代为修正，得到 %+v", w)
	}
	if n := len(loaded.Waivers("s1")); n != 1 {
		t.Fatalf("不得删除或新增免修申请，得到 %d 条", n)
	}

	// 调用方自行修正归属（为 s1 补齐要求 r9）后，同一路径即可正常保存。
	if _, _, err := loaded.AddCourse("c3", "概率统计", 2); err != nil {
		t.Fatalf("登记课程失败：%v", err)
	}
	if _, _, err := loaded.AddRequirement("s1", "r9", "c3"); err != nil {
		t.Fatalf("补齐要求失败：%v", err)
	}
	if err := loaded.Save(path); err != nil {
		t.Fatalf("修正归属后应能正常保存：%v", err)
	}
	final, _, err := Load(path)
	if err != nil {
		t.Fatalf("修正归属后的文件应可正常读取：%v", err)
	}
	if w := final.Waiver("s1", "w1"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r9" {
		t.Fatalf("修正后的免修应原样落盘，得到 %+v", w)
	}
}

// TestSaveWaiverOwnershipReflectsCurrentContent 归属判定只反映本次待保存记录
// 的当前内容：修改前曾经匹配不能作为放行依据；调用方把编号改回真实存在的
// 要求后，同一份记录又能正常保存。
func TestSaveWaiverOwnershipReflectsCurrentContent(t *testing.T) {
	s := saveWaiverStore(t)
	w1 := mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)

	// 改动前保存一切正常。
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("改动前的正常记录应能保存：%v", err)
	}

	// 改走目标要求后：不能因为 w1 曾经合法就继续放行。
	w1.ReqID = "r9"
	if err := s.Save(path); err == nil {
		t.Fatal("目标要求被改走后必须拒绝保存，不能凭修改前的匹配放行")
	}

	// 改回真实存在的 r1 后，同一份记录再次正常保存。
	w1.ReqID = "r1"
	if err := s.Save(path); err != nil {
		t.Fatalf("归属修正后应能正常保存：%v", err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("修正后的文件应可正常读取：%v", err)
	}
	if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("修正后 s1 应凭 w1 得 4 学分，得到 %+v", rep)
	}
}

// TestSaveWaiverOwnershipDoesNotMutateMemory 拒绝保存不得改动待保存内存记录
// 的任何内容：归属无效的申请、其他有效免修、被拒绝/已撤销历史都保持提交时
// 的样子，也不跳过坏申请只保存其他记录。
func TestSaveWaiverOwnershipDoesNotMutateMemory(t *testing.T) {
	s := saveWaiverStore(t)
	mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
	w2 := mustWaiver(t, s, "s1", "r2", "w2", "课程已修过", WaiverApproved)
	// 其他学生的合法有效免修与一条已撤销历史也不受影响。
	mustWaiver(t, s, "s2", "r1", "w1", "竞赛获奖", WaiverApproved)
	if _, _, err := s.RevokeWaiver("s2", "w1", "材料作废"); err != nil {
		t.Fatal(err)
	}
	// 调用方把 w2 的目标改到不存在的要求。
	w2.ReqID = "r9"

	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err == nil {
		t.Fatal("有效免修指向不存在的要求时必须拒绝保存")
	}

	if w := s.Waiver("s1", "w2"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r9" || w.Basis != "课程已修过" {
		t.Fatalf("w2 应保持调用方改成的样子，不得代为修正或降级，得到 %+v", w)
	}
	if w := s.Waiver("s1", "w1"); w == nil || w.Status != WaiverApproved || w.ReqID != "r1" {
		t.Fatalf("w1 不得被改动，得到 %+v", w)
	}
	if w := s.Waiver("s2", "w1"); w == nil || w.Status != WaiverRevoked || w.Reason != "材料作废" {
		t.Fatalf("s2 的已撤销历史不得被改动，得到 %+v", w)
	}
	if n := len(s.waivers); n != 3 {
		t.Fatalf("不得删除任何免修记录，得到 %d 条", n)
	}
	// 不得自动创建要求、不得转移学生归属。
	if r := s.Requirement("s1", "r9"); r != nil {
		t.Fatalf("不得自动创建要求 r9，得到 %+v", r)
	}
	if n := len(s.Requirements("s1")); n != 2 {
		t.Fatalf("s1 的要求应仍为 2 项，得到 %d", n)
	}
}

// TestSaveWaiverOwnershipLegalRecordsStillSave 不受归属检查限制的记录继续正常
// 保存：已拒绝申请保留当时不存在的目标要求与原拒绝原因；已撤销申请沿用现有
// 规则；两名学生各自拥有同号要求和同号免修仍是独立的合法记录；正常有效免修
// 的学分与来源不变。
func TestSaveWaiverOwnershipLegalRecordsStillSave(t *testing.T) {
	t.Run("已拒绝申请保留不存在的目标要求与原原因", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		// w2 因目标 r9 不存在被正常拒绝：目标与原因都是合法历史。
		w2 := mustWaiver(t, s, "s1", "r9", "w2", "课程已修过", WaiverRejected)
		if w2.Reason == "" {
			t.Fatal("被拒绝的申请必须保留拒绝原因")
		}

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("已拒绝申请的历史不应触发归属检查，应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		w := loaded.Waiver("s1", "w2")
		if w == nil || w.Status != WaiverRejected || w.ReqID != "r9" ||
			w.Reason == "" {
			t.Fatalf("已拒绝历史应保留原目标与原原因，得到 %+v", w)
		}
		rep := loaded.CheckStudent("s1")
		if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver.ID != "w2" {
			t.Fatalf("被拒绝历史应原样列出，得到 %+v", rep.RejectedWaivers)
		}
		if rep.TotalCredits != 4 || rep.Requirements[0].WaiverID != "w1" {
			t.Fatalf("r1 应凭 w1 计 4 学分，得到 %+v", rep)
		}
	})

	t.Run("已撤销申请沿用现有规则", func(t *testing.T) {
		s := saveWaiverStore(t)
		mustWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", WaiverApproved)
		if _, _, err := s.RevokeWaiver("s1", "w1", "材料作废"); err != nil {
			t.Fatal(err)
		}

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("已撤销历史应照常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		if w := loaded.Waiver("s1", "w1"); w == nil || w.Status != WaiverRevoked {
			t.Fatalf("已撤销历史应原样保留，得到 %+v", w)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 0 ||
			len(rep.RevokedWaivers) != 1 {
			t.Fatalf("撤销后不计学分但保留撤销历史，得到 %+v", rep)
		}
	})

	t.Run("两名学生各自同号要求同号免修", func(t *testing.T) {
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
}
