package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“保存时不得把同一学生名下同一要求的两份有效免修写入记录文件”。
// 正常申请流程用 validWaiver 挡住第二份有效免修，Load 也拒绝这类文件，冲突
// 只会出现在“程序取得免修记录后直接修改其内容”这条路径上（把另一份申请改
// 成有效，或把有效申请的目标改到已由免修满足的要求），因此用例先经公开接口
// 建立结构完整的记录，再直接改免修内容，聚焦 Save 这一路径：
//   - 同一学生名下同一要求有两份不同编号、状态为有效的免修时（依据相同、
//     要求另有通过修读、冲突申请之间夹着其他合法历史，结论都一样），整次
//     保存必须拒绝：错误说明是有效免修冲突，并点名目标文件、所属学生、
//     要求编号与冲突的两份免修编号；
//   - 目标文件原本不存在时不创建记录文件，也不残留临时文件；已有文件的全部
//     字节原样保留、仍可正常读取与核对；
//   - 待保存的内存记录保持提交时的内容：不删除申请，不替调用方选定一份有效
//     免修，也不改状态、目标要求或依据；调用方消除冲突后可继续保存；
//   - 冲突只按所属学生与目标要求的完整编号判断：不同学生各自拥有同号要求、
//     同号免修合法，同一学生的不同要求各有一份有效免修合法，编号前后有空白
//     的要求互不合并，失效申请（已拒绝/已撤销）不占用有效名额。

// saveWaiverConflictStore 建立两名学生、两门课程与三项要求，供用例自行构造
// 有效免修冲突。
func saveWaiverConflictStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustCourse(t, s, "c2", "线性代数", 3)
	mustReq(t, s, "s1", "r1", "c1")
	mustReq(t, s, "s1", "r2", "c2")
	mustReq(t, s, "s2", "r1", "c1")
	return s
}

// mustApprovedWaiver 经公开接口登记一份有效免修。
func mustApprovedWaiver(t *testing.T, s *Store, student, req, id, basis string) *Waiver {
	t.Helper()
	w, _, err := s.ApplyWaiver(student, req, id, basis)
	if err != nil || w.Status != WaiverApproved {
		t.Fatalf("有效免修构造失败：%+v %v", w, err)
	}
	return w
}

// assertSaveWaiverConflictError 断言保存错误说明是有效免修冲突，并点名目标
// 文件、所属学生、要求编号与冲突的两份免修编号，返回解析后的类型化错误。
func assertSaveWaiverConflictError(t *testing.T, err error, path string,
	student, req, first, second string) *duplicateApprovedWaiverError {
	t.Helper()
	if err == nil {
		t.Fatalf("同一要求存在两份有效免修（%s 与 %s）时必须拒绝保存", first, second)
	}
	var dwe *duplicateApprovedWaiverError
	if !errors.As(err, &dwe) {
		t.Fatalf("应返回 *duplicateApprovedWaiverError，得到 %T：%v", err, err)
	}
	if dwe.student != student || dwe.req != req ||
		dwe.first != first || dwe.second != second {
		t.Fatalf("类型化错误定位不符：want=(%s,%s,%s,%s)，得到 %+v",
			student, req, first, second, dwe)
	}
	msg := err.Error()
	for _, want := range []string{"有效免修冲突", path, student, req, first, second} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误应说明有效免修冲突并点名目标文件、学生、要求与双方免修（缺 %q），得到：%v",
				want, err)
		}
	}
	return dwe
}

// assertNoFileAndNoTemp 断言目标文件未被创建且目录下没有残留临时文件。
func assertNoFileAndNoTemp(t *testing.T, path string) {
	t.Helper()
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("拒绝保存后不得创建记录文件，stat err=%v", statErr)
	}
	leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
	}
}

// TestSaveRejectsDuplicateApprovedWaivers 覆盖各类同一要求并存两份有效免修的
// 组合：构造路径、依据异同、是否有通过修读、排列位置都不能改变拒绝结论。
func TestSaveRejectsDuplicateApprovedWaivers(t *testing.T) {
	t.Run("把另一份申请改成有效", func(t *testing.T) {
		s := saveWaiverConflictStore(t)
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		// 第二份申请因已有有效免修被业务拒绝，程序取得记录后把它改成有效。
		w2, _, err := s.ApplyWaiver("s1", "r1", "w2", "课程互认")
		if err != nil || w2.Status != WaiverRejected {
			t.Fatalf("第二份申请应被业务拒绝：%+v %v", w2, err)
		}
		w2.Status = WaiverApproved

		path := filepath.Join(t.TempDir(), "records.json")
		err = s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
		assertNoFileAndNoTemp(t, path)
	})

	t.Run("把有效申请的目标改到已由免修满足的要求", func(t *testing.T) {
		s := saveWaiverConflictStore(t)
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		w2 := mustApprovedWaiver(t, s, "s1", "r2", "w2", "课程互认")
		// 程序取得记录后把 w2 的目标改到已由 w1 满足的 r1。
		w2.ReqID = "r1"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
		assertNoFileAndNoTemp(t, path)
	})

	t.Run("两份申请依据相同仍算两份免修", func(t *testing.T) {
		s := saveWaiverConflictStore(t)
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		w2 := mustApprovedWaiver(t, s, "s1", "r2", "w2", "竞赛获奖")
		w2.ReqID = "r1" // 依据与 w1 完全相同，但编号不同，仍是两份免修。

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
		assertNoFileAndNoTemp(t, path)
	})

	t.Run("要求另有通过修读不能掩盖冲突", func(t *testing.T) {
		s := saveWaiverConflictStore(t)
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		w2 := mustApprovedWaiver(t, s, "s1", "r2", "w2", "课程互认")
		w2.ReqID = "r1" // r1 已凭 e1 通过获得学分，冲突仍然成立。

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
		assertNoFileAndNoTemp(t, path)
	})

	t.Run("冲突申请之间夹着其他合法历史", func(t *testing.T) {
		s := saveWaiverConflictStore(t)
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		// 夹在中间的合法历史：另一学生的有效免修与一份被拒绝的申请。
		mustApprovedWaiver(t, s, "s2", "r1", "w9", "他校已修")
		if w, _, err := s.ApplyWaiver("s1", "r1", "w8", "重复申请"); err != nil ||
			w.Status != WaiverRejected {
			t.Fatalf("被拒绝免修构造失败：%+v %v", w, err)
		}
		w2 := mustApprovedWaiver(t, s, "s1", "r2", "w2", "课程互认")
		w2.ReqID = "r1"

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveWaiverConflictError(t, err, path, "s1", "r1", "w1", "w2")
		assertNoFileAndNoTemp(t, path)
	})
}

// TestSaveDuplicateApprovedWaiversKeepsExistingFileUntouched 目标文件已存在且
// 内容完好时，含有效免修冲突的保存必须失败，原文件逐字节保留、仍可读取核对；
// 冲突的内存记录保持提交时的内容；调用方自行消除冲突后可继续保存。
func TestSaveDuplicateApprovedWaiversKeepsExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	// 先落一份完好文件：s1 的 r1 由有效免修 w1 满足（4 学分）。
	good := saveWaiverConflictStore(t)
	mustApprovedWaiver(t, good, "s1", "r1", "w1", "竞赛获奖")
	if err := good.Save(path); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// 另一份记录中，程序把 w2 改成同样满足 r1 的有效免修，尝试覆盖同一路径。
	bad := saveWaiverConflictStore(t)
	mustApprovedWaiver(t, bad, "s1", "r1", "w1", "竞赛获奖")
	w2 := mustApprovedWaiver(t, bad, "s1", "r2", "w2", "课程互认")
	w2.ReqID = "r1"
	saveErr := bad.Save(path)
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
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("原文件应仍可正常读取：existed=%v err=%v", existed, err)
	}
	if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("原文件核对结果应保持不变，得到 %+v", rep)
	}
	if w := loaded.Waiver("s1", "w2"); w != nil {
		t.Fatalf("坏记录中的 w2 不得落盘，得到 %+v", w)
	}

	// 内存中的冲突记录保持提交时的内容：不删除申请，不替调用方选定一份有效
	// 免修，也不改状态、目标要求或依据。
	if n := len(bad.Waivers("s1")); n != 2 {
		t.Fatalf("不得删除 s1 的任何免修申请，得到 %d 份", n)
	}
	if w := bad.Waiver("s1", "w1"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r1" || w.Basis != "竞赛获奖" {
		t.Fatalf("w1 应保持提交时的内容，得到 %+v", w)
	}
	if w := bad.Waiver("s1", "w2"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r1" || w.Basis != "课程互认" {
		t.Fatalf("w2 应保持提交时的内容，得到 %+v", w)
	}

	// 由调用方自行消除冲突（撤销 w2）后，同一路径即可正常保存。
	w2.Status = WaiverRevoked
	if err := bad.Save(path); err != nil {
		t.Fatalf("调用方消除冲突后应能正常保存：%v", err)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("消除冲突后的文件应可正常读取：%v", err)
	}
	if w := reloaded.Waiver("s1", "w2"); w == nil || w.Status != WaiverRevoked {
		t.Fatalf("消除冲突后的修改应原样落盘，得到 %+v", w)
	}
	if rep := reloaded.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("消除冲突后 r1 仍应由 w1 满足，得到 %+v", rep)
	}
}

// TestSaveLegalApprovedWaiverArrangementsAccepted 合法的有效免修安排继续正常
// 保存，核对规则保持不变：
//   - 不同学生各自拥有同号要求、同号免修；
//   - 同一学生的不同要求各有一份有效免修；
//   - 编号前后有空白的要求是不同要求，互不合并；
//   - 同一要求下一份有效免修与已拒绝、已撤销历史共存，失效申请不占用有效
//     名额，所有历史按原内容保留。
func TestSaveLegalApprovedWaiverArrangementsAccepted(t *testing.T) {
	t.Run("不同学生同号要求同号免修", func(t *testing.T) {
		s := saveWaiverConflictStore(t)
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		mustApprovedWaiver(t, s, "s2", "r1", "w1", "他校已修")

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
		if rep := loaded.CheckStudent("s2"); rep.TotalCredits != 4 ||
			rep.Requirements[0].WaiverID != "w1" {
			t.Fatalf("s2 应凭本人 w1 得 4 学分，得到 %+v", rep)
		}
		if loaded.Waiver("s1", "w1") == loaded.Waiver("s2", "w1") {
			t.Fatal("两名学生的同号免修应仍是两份独立申请")
		}
	})

	t.Run("同一学生不同要求各一份有效免修", func(t *testing.T) {
		s := saveWaiverConflictStore(t)
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		mustApprovedWaiver(t, s, "s1", "r2", "w2", "课程互认")

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("不同要求各一份有效免修应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 7 || len(rep.Unmet) != 0 {
			t.Fatalf("两项要求应各计一份学分共 7 学分，得到学分=%d 未满足=%v",
				rep.TotalCredits, rep.Unmet)
		}
	})

	t.Run("编号前后有空白的要求互不合并", func(t *testing.T) {
		s := NewStore()
		mustStudent(t, s, "s1")
		mustCourse(t, s, "c1", "高等数学", 4)
		mustCourse(t, s, "c2", "线性代数", 3)
		mustReq(t, s, "s1", "r1", "c1")
		mustReq(t, s, "s1", " r1 ", "c2") // 完整编号不同，是另一项要求。
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		mustApprovedWaiver(t, s, "s1", " r1 ", "w2", "课程互认")

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("编号含空白的要求是不同要求，各一份有效免修应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 7 {
			t.Fatalf("两项要求应各计一份学分共 7 学分，得到 %d", rep.TotalCredits)
		}
		if loaded.Waiver("s1", "w1").ReqID != "r1" ||
			loaded.Waiver("s1", "w2").ReqID != " r1 " {
			t.Fatalf("带空白编号不得被修剪或合并：%+v %+v",
				loaded.Waiver("s1", "w1"), loaded.Waiver("s1", "w2"))
		}
	})

	t.Run("一份有效免修与已拒绝已撤销历史共存", func(t *testing.T) {
		s := saveWaiverConflictStore(t)
		mustApprovedWaiver(t, s, "s1", "r1", "w1", "竞赛获奖")
		// 已拒绝历史：同一要求的重复申请被业务拒绝，原因保留。
		if w, _, err := s.ApplyWaiver("s1", "r1", "w2", "重复申请"); err != nil ||
			w.Status != WaiverRejected {
			t.Fatalf("被拒绝免修构造失败：%+v %v", w, err)
		}
		// 已撤销历史：另一要求上曾经有效的免修被撤销。
		mustApprovedWaiver(t, s, "s1", "r2", "w3", "旧依据")
		if _, _, err := s.RevokeWaiver("s1", "w3", "材料无法核实"); err != nil {
			t.Fatal(err)
		}

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("失效申请不占用有效名额，应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		// 所有历史按原内容保留，有效免修仍说明当前来源。
		if n := len(loaded.Waivers("s1")); n != 3 {
			t.Fatalf("三份免修历史都应保留，得到 %d 份", n)
		}
		if w := loaded.Waiver("s1", "w2"); w == nil || w.Status != WaiverRejected ||
			w.Basis != "重复申请" {
			t.Fatalf("已拒绝历史应按原内容保留，得到 %+v", w)
		}
		if w := loaded.Waiver("s1", "w3"); w == nil || w.Status != WaiverRevoked ||
			w.Basis != "旧依据" {
			t.Fatalf("已撤销历史应按原内容保留，得到 %+v", w)
		}
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 4 || rep.Requirements[0].WaiverID != "w1" {
			t.Fatalf("r1 应仍由唯一有效免修 w1 满足，得到 %+v", rep)
		}
	})
}
