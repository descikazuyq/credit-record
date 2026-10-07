package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/descikazuyq/credit-record/credit"
)

// 本文件从命令行入口回归“保存时每份有效免修都必须归属到本人名下的真实
// 要求”。这种记录无法经正常命令产生（waiver 命令只会为本人名下真实存在的
// 要求生效免修，目标不存在的申请会被记为已拒绝），它来自“直接使用课程记录
// 功能的程序取得记录后修改了免修内容”：本用例就模拟这样的程序——通过
// credit 包的公开接口取得免修、改目标要求或所属学生，再调用保存功能。保存
// 功能必须：
//   - 直接返回错误：点名目标记录文件、免修编号、所属学生编号与目标要求编号，
//     说明学生不存在或本人名下不存在该要求，不把可用文件替换成随后被判损坏
//     的文件；
//   - 目标原本不存在时不创建记录文件、不残留临时文件；已有文件全部字节原样
//     保留，命令行仍能正常读取与核对；
//   - 待保存内存记录保持提交时的内容，程序可自行修正归属后再保存；
//   - 已拒绝申请保留当时不存在的目标要求与原原因照常保存。

// TestProgramSaveDanglingApprovedWaiverNotCreated 模拟直接调用记录功能的
// 程序：目标文件原本不存在，程序把 s1 的有效免修 w1 的目标改成不存在的
// 要求 r9 后保存，必须收到错误，错误点名目标文件、免修 w1、学生 s1 与
// 要求 r9 并说明本人名下不存在该要求；不创建记录文件，也不残留临时文件。
// 随后命令行访问该路径应视为无记录，而不是读到损坏文件。
func TestProgramSaveDanglingApprovedWaiverNotCreated(t *testing.T) {
	s := newWaiverProgramStore(t)
	w1 := mustProgramWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", credit.WaiverApproved)
	// 程序取得记录后把目标改成 s1 名下并不存在的要求。
	w1.ReqID = "r9"

	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	err := s.Save(file)
	if err == nil {
		t.Fatal("有效免修指向不存在的要求时必须拒绝保存")
	}
	msg := err.Error()
	for _, want := range []string{file, "有效免修", "s1", "w1", "r9", "名下"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("保存错误应点名目标文件、免修、学生与目标要求并说明本人名下不存在（缺 %q），得到：%v",
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

	// 内存记录保持提交时的内容：w1 仍是程序改成的样子，程序可自行修正归属。
	w := s.Waiver("s1", "w1")
	if w == nil || w.Status != credit.WaiverApproved || w.ReqID != "r9" {
		t.Fatalf("w1 应保持程序改成的样子，保存功能不得代为修正，得到 %+v", w)
	}
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("不得删除或新增免修申请，得到 %d 条", n)
	}
	w.ReqID = "r1"
	if err := s.Save(file); err != nil {
		t.Fatalf("程序修正归属后应能正常保存：%v", err)
	}
}

// TestProgramSaveDanglingApprovedWaiverKeepsUsableFile 目标文件已存在且完好
// 时，程序取得记录后把有效免修的所属学生改成未登记编号再保存到同一路径：
// 保存失败并说明学生不存在，已有文件全部字节原样保留，命令行仍能正常读取
// 与核对，绝不会把可用文件替换成损坏文件。
func TestProgramSaveDanglingApprovedWaiverKeepsUsableFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")

	// 先落一份完好文件：s1 的 r1 由有效免修 w1 满足（4 学分）。
	good := newWaiverProgramStore(t)
	mustProgramWaiver(t, good, "s1", "r1", "w1", "竞赛获奖", credit.WaiverApproved)
	if err := good.Save(file); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	// 程序重新取得这份记录，把 w1 的所属学生改成未登记的 s9。
	loaded, existed, err := credit.Load(file)
	if err != nil || !existed {
		t.Fatalf("前置文件应正常读取：existed=%v err=%v", existed, err)
	}
	w1 := loaded.Waiver("s1", "w1")
	if w1 == nil {
		t.Fatal("前置文件中应存在 s1 的免修 w1")
	}
	w1.StudentID = "s9"
	saveErr := loaded.Save(file)
	if saveErr == nil {
		t.Fatal("有效免修的所属学生不存在时必须拒绝保存")
	}
	for _, want := range []string{file, "有效免修", "s9", "w1", "r1", "不存在"} {
		if !strings.Contains(saveErr.Error(), want) {
			t.Fatalf("保存错误应点名目标文件、免修、学生与目标要求并说明学生不存在（缺 %q），得到：%v",
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

	// 命令行仍能正常读取旧文件：s1 凭 w1 得 4 学分。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("旧文件中 s1 的核对结果应保持不变，code=%d out=%q", code, out)
	}
}

// TestProgramSaveRejectedWaiverKeepsDanglingTarget 程序经正常命令产生的
// 已拒绝申请（目标要求当时不存在）连同原拒绝原因照常保存：归属检查只针对
// 有效免修，不让这样的历史记录丢失。
func TestProgramSaveRejectedWaiverKeepsDanglingTarget(t *testing.T) {
	s := newWaiverProgramStore(t)
	mustProgramWaiver(t, s, "s1", "r1", "w1", "竞赛获奖", credit.WaiverApproved)
	// w2 因目标 r9 不存在被正常拒绝：目标与原因都是合法历史。
	w2 := mustProgramWaiver(t, s, "s1", "r9", "w2", "课程已修过", credit.WaiverRejected)
	if w2.Reason == "" {
		t.Fatal("被拒绝的申请必须保留拒绝原因")
	}

	file := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(file); err != nil {
		t.Fatalf("已拒绝申请的历史不应触发归属检查，应正常保存：%v", err)
	}

	// 命令行正常读取：s1 凭 w1 得 4 学分，被拒绝的 w2 连同原因列出。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "w2") {
		t.Fatalf("核对报告应列出被拒绝的 w2 且学分不变，code=%d out=%q", code, out)
	}
}
