package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/descikazuyq/credit-record/credit"
)

// 本文件从命令行入口回归“保存时不得把两份不同修读的已提交成绩写成共用同一个
// 正整数顺序编号”。这种冲突无法经正常命令产生（编号由计数器递增分配），它来自
// “直接使用课程记录功能的程序取得记录后修改了成绩顺序编号”：本用例就模拟这样
// 的程序——通过 credit 包的公开接口取得修读、改成同号，再调用保存功能。保存
// 功能必须：
//   - 直接返回错误：点名目标记录文件、重复的顺序编号与冲突双方（完整学生编号 +
//     修读编号），不把可用文件替换成随后被判损坏的文件；
//   - 目标原本不存在时不创建记录文件、不残留临时文件；已有文件全部字节原样
//     保留，命令行仍能正常读取与核对；
//   - 待保存内存记录保持提交时的内容，程序可自行改号后再保存；
//   - 多条选课共用编号 0、跨学生同号修读但编号不同时照常保存。

// newSeqProgramStore 像直接调用记录功能的程序一样建立记录：两名学生各有一份
// 编号为 e1 的修读（另有 s1 的 e2），用例随后提交成绩并改出顺序编号冲突。
func newSeqProgramStore(t *testing.T) *credit.Store {
	t.Helper()
	s := credit.NewStore()
	for _, step := range []func() error{
		func() error { _, _, e := s.AddStudent("s1"); return e },
		func() error { _, _, e := s.AddStudent("s2"); return e },
		func() error { _, _, e := s.AddCourse("c1", "高等数学", 4); return e },
		func() error { _, _, e := s.AddRequirement("s1", "r1", "c1"); return e },
		func() error { _, _, e := s.AddRequirement("s2", "r1", "c1"); return e },
		func() error { _, _, e := s.AddEnrollment("s1", "r1", "2024春", "e1"); return e },
		func() error { _, _, e := s.AddEnrollment("s2", "r1", "2024春", "e1"); return e },
		func() error { _, _, e := s.AddEnrollment("s1", "r1", "2024秋", "e2"); return e },
	} {
		if err := step(); err != nil {
			t.Fatalf("构造前置记录失败：%v", err)
		}
	}
	return s
}

// TestProgramSaveDuplicateResultSeqNotCreated 模拟直接调用记录功能的程序：目标
// 文件原本不存在，程序把两名学生各一份 e1 的成绩改成顺序编号 7 后保存，必须
// 收到错误，错误点名目标文件、重复编号 7 与冲突双方（s1/e1、s2/e1）；不创建
// 记录文件，也不残留临时文件。随后命令行访问该路径应视为无记录，而不是读到
// 损坏文件。
func TestProgramSaveDuplicateResultSeqNotCreated(t *testing.T) {
	s := newSeqProgramStore(t)
	if _, _, err := s.SubmitResult("s1", "e1", credit.Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s2", "e1", credit.Failed); err != nil {
		t.Fatal(err)
	}
	// 程序取得修读记录后，把两份成绩改成同一正整数顺序编号。
	s.Enrollment("s1", "e1").ResultSeq = 7
	s.Enrollment("s2", "e1").ResultSeq = 7

	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	err := s.Save(file)
	if err == nil {
		t.Fatal("两份不同修读的成绩共用顺序编号 7 时必须拒绝保存")
	}
	msg := err.Error()
	for _, want := range []string{
		file,
		"顺序编号 7",
		"s1", "e1",
		"s2", "e1",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("保存错误应点名目标文件、重复编号 7 与冲突双方（缺 %q），得到：%v",
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

	// 内存记录保持提交时的内容：两份修读与成绩都在，程序可自行改号后再保存。
	if e := s.Enrollment("s1", "e1"); e == nil || e.Result != credit.Passed || e.ResultSeq != 7 {
		t.Fatalf("s1 的 e1 应保持通过、编号 7，得到 %+v", e)
	}
	if e := s.Enrollment("s2", "e1"); e == nil || e.Result != credit.Failed || e.ResultSeq != 7 {
		t.Fatalf("s2 的 e1 应保持未通过、编号 7，得到 %+v", e)
	}
	if n := len(s.Enrollments("s1")); n != 2 {
		t.Fatalf("不得删除 s1 的修读，得到 %d 条", n)
	}
}

// TestProgramSaveDuplicateResultSeqKeepsUsableFile 目标文件已存在且完好时，程序
// 取得记录后改出顺序编号冲突再保存到同一路径：保存失败，已有文件全部字节原样
// 保留，命令行仍能正常读取与核对，绝不会把可用文件替换成损坏文件。
func TestProgramSaveDuplicateResultSeqKeepsUsableFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")

	// 先落一份完好文件（s1/e1 通过，s2/e1 仍选课）并记录其字节。
	good := newSeqProgramStore(t)
	if _, _, err := good.SubmitResult("s1", "e1", credit.Passed); err != nil {
		t.Fatal(err)
	}
	if err := good.Save(file); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	// 程序重新取得这份记录，再给 s2/e1 提交成绩，并把两份成绩改成同一编号 1。
	loaded, existed, err := credit.Load(file)
	if err != nil || !existed {
		t.Fatalf("前置文件应正常读取：existed=%v err=%v", existed, err)
	}
	if _, _, err := loaded.SubmitResult("s2", "e1", credit.Failed); err != nil {
		t.Fatal(err)
	}
	loaded.Enrollment("s1", "e1").ResultSeq = 1
	loaded.Enrollment("s2", "e1").ResultSeq = 1
	saveErr := loaded.Save(file)
	if saveErr == nil {
		t.Fatal("改出顺序编号冲突后保存必须失败")
	}
	for _, want := range []string{file, "顺序编号 1", "s1", "s2", "e1"} {
		if !strings.Contains(saveErr.Error(), want) {
			t.Fatalf("保存错误应点名目标文件、编号 1 与双方（缺 %q），得到：%v",
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

	// 命令行仍能正常读取旧文件：s1 凭 e1 得 4 学分；s2 的未通过成绩没有落盘，
	// 旧文件里 s2 的 e1 仍是选课、0 学分、要求未满足。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("旧文件中 s1 的核对结果应保持不变，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s2")
	if code != exitOK || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("旧文件中 s2 的 e1 应仍是选课、0 学分未满足，code=%d out=%q", code, out)
	}
	if strings.Contains(out, "通过修读") {
		t.Fatalf("s2 的未通过成绩不得落盘，out=%q", out)
	}
}

// TestProgramSaveSeqConflictSameStudentBothCombos 同一学生名下两份不同修读的
// 成绩被改成同号时，无论一通过一未通过还是两份都未通过，都拒绝整次保存并
// 分别点名两份修读；已有文件保持原样。
func TestProgramSaveSeqConflictSameStudentBothCombos(t *testing.T) {
	cases := []struct {
		label         string
		first, second credit.Result
	}{
		{"一通过一未通过", credit.Passed, credit.Failed},
		{"两份都未通过", credit.Failed, credit.Failed},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "records.json")

			// 先落一份只有 s1/e1 选课的完好文件，保证已有内容不被覆盖。
			seed := credit.NewStore()
			if _, _, err := seed.AddStudent("s1"); err != nil {
				t.Fatal(err)
			}
			if _, _, err := seed.AddCourse("c1", "数学", 4); err != nil {
				t.Fatal(err)
			}
			if _, _, err := seed.AddRequirement("s1", "r1", "c1"); err != nil {
				t.Fatal(err)
			}
			if _, _, err := seed.AddEnrollment("s1", "r1", "2024春", "e1"); err != nil {
				t.Fatal(err)
			}
			if err := seed.Save(file); err != nil {
				t.Fatalf("前置保存失败：%v", err)
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			s := newSeqProgramStore(t)
			if _, _, err := s.SubmitResult("s1", "e1", c.first); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.SubmitResult("s1", "e2", c.second); err != nil {
				t.Fatal(err)
			}
			s.Enrollment("s1", "e1").ResultSeq = 3
			s.Enrollment("s1", "e2").ResultSeq = 3
			saveErr := s.Save(file)
			if saveErr == nil {
				t.Fatalf("%s：同一学生两份修读共用编号 3 时必须拒绝保存", c.label)
			}
			if !strings.Contains(saveErr.Error(), file) ||
				!strings.Contains(saveErr.Error(), "顺序编号 3") ||
				!strings.Contains(saveErr.Error(), "e1") ||
				!strings.Contains(saveErr.Error(), "e2") {
				t.Fatalf("%s：错误应点名目标文件、编号 3 与两份修读，得到：%v",
					c.label, saveErr)
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(raw) {
				t.Fatalf("%s：拒绝保存后已有文件必须原样保留", c.label)
			}
		})
	}
}

// TestProgramSaveLegalSeqArrangementsStillWorks 合法编号安排经直接调用保存功能
// 仍正常落盘并可被命令行读取：多条选课编号 0、跨学生同号修读但编号不同。
func TestProgramSaveLegalSeqArrangementsStillWorks(t *testing.T) {
	s := newSeqProgramStore(t)
	// s1/e1 通过（1），s2/e1 未通过（2），s1/e2 仍是选课（0）。
	if _, _, err := s.SubmitResult("s1", "e1", credit.Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s2", "e1", credit.Failed); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(file); err != nil {
		t.Fatalf("编号互不相同的合法记录应正常保存：%v", err)
	}
	leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(file), ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("正常保存不应残留临时文件：%v", leftover)
	}

	// 命令行正常读取：s1 得 4 学分、来源本人 e1；s2 未通过、0 学分。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s1 核对结果应保持，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s2")
	if code != exitOK || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("s2 的 e1 未通过，应 0 学分未满足，code=%d out=%q", code, out)
	}
}
