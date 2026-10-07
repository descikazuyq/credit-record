package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/descikazuyq/credit-record/credit"
)

// 本文件从命令行入口回归“保存时同一学生名下的同一项要求不得同时有两份有效
// 免修”。这种冲突无法经正常命令产生（waiver 命令会把已有有效免修要求上的
// 新申请记为已拒绝），它来自“直接使用课程记录功能的程序取得记录后修改了
// 免修内容”：本用例就模拟这样的程序——通过 credit 包的公开接口取得免修、
// 把另一份申请改成有效或改目标要求，再调用保存功能。保存功能必须：
//   - 直接返回错误：说明是有效免修冲突，并点名目标记录文件、所属学生、要求
//     编号与冲突的两份免修编号，不把可用文件替换成随后被判损坏的文件；
//   - 目标原本不存在时不创建记录文件、不残留临时文件；已有文件全部字节原样
//     保留，命令行仍能正常读取与核对；
//   - 待保存内存记录保持提交时的内容，程序可自行消除冲突后再保存；
//   - 不同学生各自的同号有效免修、同一学生不同要求各一份有效免修照常保存。

// newWaiverProgramStore 像直接调用记录功能的程序一样建立记录：s1 名下有要求
// r1（4 学分课程 c1）与 r2（3 学分课程 c2），s2 名下有同号要求 r1（c2）。
func newWaiverProgramStore(t *testing.T) *credit.Store {
	t.Helper()
	s := credit.NewStore()
	for _, step := range []func() error{
		func() error { _, _, e := s.AddStudent("s1"); return e },
		func() error { _, _, e := s.AddStudent("s2"); return e },
		func() error { _, _, e := s.AddCourse("c1", "高等数学", 4); return e },
		func() error { _, _, e := s.AddCourse("c2", "线性代数", 3); return e },
		func() error { _, _, e := s.AddRequirement("s1", "r1", "c1"); return e },
		func() error { _, _, e := s.AddRequirement("s1", "r2", "c2"); return e },
		func() error { _, _, e := s.AddRequirement("s2", "r1", "c2"); return e },
	} {
		if err := step(); err != nil {
			t.Fatalf("构造前置记录失败：%v", err)
		}
	}
	return s
}

// mustProgramWaiver 经公开接口提交免修申请并断言得到的状态。
func mustProgramWaiver(t *testing.T, s *credit.Store, student, req, id, basis string,
	want credit.WaiverStatus) *credit.Waiver {
	t.Helper()
	w, _, err := s.ApplyWaiver(student, req, id, basis)
	if err != nil {
		t.Fatalf("ApplyWaiver(%q,%q,%q) 失败：%v", student, req, id, err)
	}
	if w.Status != want {
		t.Fatalf("免修 %s 状态应为 %s，得到 %s", id, want, w.Status)
	}
	return w
}

// TestProgramSaveDuplicateApprovedWaiverNotCreated 模拟直接调用记录功能的程序：
// 目标文件原本不存在，程序把 s1/r1 上被正常拒绝的 w2 直接改成有效后保存，
// 必须收到错误，错误说明有效免修冲突并点名目标文件、学生 s1、要求 r1 与
// 冲突的 w1、w2；不创建记录文件，也不残留临时文件。随后命令行访问该路径应
// 视为无记录，而不是读到损坏文件。
func TestProgramSaveDuplicateApprovedWaiverNotCreated(t *testing.T) {
	s := newWaiverProgramStore(t)
	mustProgramWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", credit.WaiverApproved)
	// 第二份申请被正常拒绝并记入历史，程序取得记录后直接把它改成有效。
	w2 := mustProgramWaiver(t, s, "s1", "r1", "w2", "课程已修过", credit.WaiverRejected)
	w2.Status = credit.WaiverApproved

	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	err := s.Save(file)
	if err == nil {
		t.Fatal("同一要求有两份有效免修 w1 与 w2 时必须拒绝保存")
	}
	msg := err.Error()
	for _, want := range []string{file, "有效免修", "s1", "r1", "w1", "w2"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("保存错误应说明有效免修冲突并点名目标文件、学生、要求与双方（缺 %q），得到：%v",
				want, err)
		}
	}
	if _, statErr := os.Stat(file); !os.IsNotExist(statErr) {
		t.Fatalf("拒绝保存后不得创建记录文件，stat err=%v", statErr)
	}
	leftover, _ := filepath.Glob(filepath.Join(dir, ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
	}

	// 命令行随后访问该路径：文件仍不存在，按“学生不存在”的业务规则退出码 1，
	// 绝不能读到一份损坏文件（退出码 2）。
	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitRejected {
		t.Fatalf("文件未创建时应按无记录处理（退出码 %d），code=%d out=%q err=%q",
			exitRejected, code, out, errText)
	}
	if strings.Contains(errText, "内容损坏") {
		t.Fatalf("拒绝保存不得留下损坏文件，err=%q", errText)
	}

	// 内存记录保持提交时的内容：两份申请都在，程序可自行消除冲突后再保存。
	if w := s.Waiver("s1", "w1"); w == nil || w.Status != credit.WaiverApproved {
		t.Fatalf("w1 应保持有效，得到 %+v", w)
	}
	if w := s.Waiver("s1", "w2"); w == nil || w.Status != credit.WaiverApproved ||
		w.ReqID != "r1" || w.Basis != "课程已修过" {
		t.Fatalf("w2 应保持程序改成的样子，保存功能不得代为选定或改回，得到 %+v", w)
	}
	if n := len(s.Waivers("s1")); n != 2 {
		t.Fatalf("不得删除 s1 的免修申请，得到 %d 条", n)
	}
	w2.Status = credit.WaiverRevoked
	if err := s.Save(file); err != nil {
		t.Fatalf("程序消除冲突后应能正常保存：%v", err)
	}
}

// TestProgramSaveDuplicateApprovedWaiverKeepsUsableFile 目标文件已存在且完好
// 时，程序取得记录后改出有效免修冲突再保存到同一路径：保存失败，已有文件
// 全部字节原样保留，命令行仍能正常读取与核对，绝不会把可用文件替换成损坏
// 文件。
func TestProgramSaveDuplicateApprovedWaiverKeepsUsableFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")

	// 先落一份完好文件：s1 的 r1 由有效免修 w1 满足（4 学分），r2 未满足。
	good := newWaiverProgramStore(t)
	mustProgramWaiver(t, good, "s1", "r1", "w1", "竞赛获奖", credit.WaiverApproved)
	if err := good.Save(file); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	// 程序重新取得这份记录，把 r2 的有效免修 w2 的目标改到已有有效免修的 r1。
	loaded, existed, err := credit.Load(file)
	if err != nil || !existed {
		t.Fatalf("前置文件应正常读取：existed=%v err=%v", existed, err)
	}
	w2 := mustProgramWaiver(t, loaded, "s1", "r2", "w2", "课程已修过", credit.WaiverApproved)
	w2.ReqID = "r1"
	saveErr := loaded.Save(file)
	if saveErr == nil {
		t.Fatal("把有效免修目标改到已由免修满足的要求后保存必须失败")
	}
	for _, want := range []string{file, "有效免修", "s1", "r1", "w1", "w2"} {
		if !strings.Contains(saveErr.Error(), want) {
			t.Fatalf("保存错误应说明有效免修冲突并点名目标文件、学生、要求与双方（缺 %q），得到：%v",
				want, saveErr)
		}
	}

	got, err := os.ReadFile(file)
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

	// 命令行仍能正常读取旧文件：s1 凭 w1 得 4 学分；w2 没有落盘。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("旧文件中 s1 的核对结果应保持不变，code=%d out=%q", code, out)
	}
	if strings.Contains(out, "w2") {
		t.Fatalf("坏记录不得落盘，旧文件中不应出现 w2，out=%q", out)
	}
}

// TestProgramSaveLegalWaiverArrangementsStillWorks 合法免修安排经直接调用保存
// 功能仍正常落盘并可被命令行读取：不同学生各自的同号有效免修、同一学生不同
// 要求各一份有效免修、有效免修与已拒绝/已撤销历史共存。
func TestProgramSaveLegalWaiverArrangementsStillWorks(t *testing.T) {
	s := newWaiverProgramStore(t)
	mustProgramWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", credit.WaiverApproved)
	mustProgramWaiver(t, s, "s1", "r2", "w2", "课程已修过", credit.WaiverApproved)
	// s2 的同号要求、同号免修是另一名学生的合法记录。
	mustProgramWaiver(t, s, "s2", "r1", "w1", "竞赛获奖", credit.WaiverApproved)
	// s1/r1 上再留一份被正常拒绝的申请：失效历史不占有效名额。
	mustProgramWaiver(t, s, "s1", "r1", "w9", "重复申请", credit.WaiverRejected)

	file := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(file); err != nil {
		t.Fatalf("合法免修安排应正常保存：%v", err)
	}
	leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(file), ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("正常保存不应残留临时文件：%v", leftover)
	}

	// 命令行正常读取：s1 两项要求各计一份共 7 学分；s2 凭本人 w1 得 3 学分。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：7") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		!strings.Contains(out, "来源为有效免修 w2") {
		t.Fatalf("s1 核对结果应保持，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s2")
	if code != exitOK || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("s2 应凭本人 w1 得 3 学分，code=%d out=%q", code, out)
	}
}
